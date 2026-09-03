package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/xiaobaitu/soloqueue/internal/agenttools/skill"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

const skillHotReloadDebounce = 250 * time.Millisecond

// skillWatcher watches both the installed-skills root and each immediate skill
// directory. ClawHub creates the directory before extracting SKILL.md, so the
// child watcher must be installed before the debounced rebuild runs.
type skillWatcher struct {
	registry *skill.SkillRegistry
	dirs     map[string]string
	log      *logger.Logger
	watcher  *fsnotify.Watcher

	closed         chan struct{}
	closeOnce      sync.Once
	done           chan struct{}
	rebuildRequest chan struct{}
	rebuildFn      func() error
}

// registerSkillHotReload starts watching installed Skill directories and
// returns an idempotent close function for runtime shutdown.
func registerSkillHotReload(reg *skill.SkillRegistry, dirs map[string]string, log *logger.Logger) func() {
	if reg == nil {
		return func() {}
	}

	sw, err := newSkillWatcher(reg, dirs, log)
	if err != nil {
		log.Warn(logger.CatApp, "skills hot-reload: cannot create watcher", "err", err.Error())
		return func() {}
	}
	go sw.run()
	return sw.Close
}

func newSkillWatcher(reg *skill.SkillRegistry, dirs map[string]string, log *logger.Logger) (*skillWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	sw := &skillWatcher{
		registry:       reg,
		dirs:           cloneSkillDirs(dirs),
		log:            log,
		watcher:        watcher,
		closed:         make(chan struct{}),
		done:           make(chan struct{}),
		rebuildRequest: make(chan struct{}, 1),
	}
	sw.rebuildFn = func() error { return sw.registry.Rebuild(sw.dirs) }

	for _, dir := range sw.dirs {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			_ = watcher.Close()
			log.Warn(logger.CatApp, "skills hot-reload: cannot create skills dir", "path", dir, "err", err.Error())
			return nil, err
		}
		info, err := os.Lstat(dir)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			_ = watcher.Close()
			if err == nil {
				err = fmt.Errorf("skills path is not a regular directory")
			}
			return nil, fmt.Errorf("invalid skills directory %s: %w", dir, err)
		}
		if err := watcher.Add(dir); err != nil {
			_ = watcher.Close()
			log.Warn(logger.CatApp, "skills hot-reload: cannot watch skills dir", "path", dir, "err", err.Error())
			return nil, err
		}
		if err := sw.watchExistingSkillDirs(dir); err != nil {
			log.Warn(logger.CatApp, "skills hot-reload: cannot enumerate skills dirs", "path", dir, "err", err.Error())
		}
	}
	return sw, nil
}

func cloneSkillDirs(dirs map[string]string) map[string]string {
	cloned := make(map[string]string, len(dirs))
	for scope, dir := range dirs {
		cloned[scope] = dir
	}
	return cloned
}

func (sw *skillWatcher) watchExistingSkillDirs(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			continue
		}
		if err := sw.watcher.Add(path); err != nil {
			sw.log.Warn(logger.CatApp, "skills hot-reload: cannot watch skill dir", "path", path, "err", err.Error())
		}
	}
	return nil
}

func (sw *skillWatcher) run() {
	defer close(sw.done)
	defer func() {
		if r := recover(); r != nil {
			sw.log.Error(logger.CatApp, "skills hot-reload goroutine panic recovered", "panic", fmt.Sprintf("%v", r))
		}
	}()

	var timer *time.Timer
	var timerCh <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		select {
		case <-sw.closed:
			return
		case evt, ok := <-sw.watcher.Events:
			if !ok {
				return
			}
			sw.handleEvent(evt)
		case err, ok := <-sw.watcher.Errors:
			if !ok {
				return
			}
			sw.log.Warn(logger.CatApp, "skills hot-reload watch error", "err", err.Error())
		case <-sw.rebuildRequest:
			if timer == nil {
				timer = time.NewTimer(skillHotReloadDebounce)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(skillHotReloadDebounce)
			}
			timerCh = timer.C
		case <-timerCh:
			timerCh = nil
			if err := sw.rebuildFn(); err != nil {
				sw.log.Warn(logger.CatApp, "skills hot-reload: rebuild failed", "err", err.Error())
				continue
			}
			sw.log.Info(logger.CatApp, "skills hot-reload completed")
		}
	}
}

func (sw *skillWatcher) handleEvent(evt fsnotify.Event) {
	if !evt.Has(fsnotify.Write) && !evt.Has(fsnotify.Create) && !evt.Has(fsnotify.Rename) && !evt.Has(fsnotify.Remove) {
		return
	}

	if root, ok := sw.skillRootFor(evt.Name); ok {
		if evt.Has(fsnotify.Create) {
			if info, err := os.Lstat(evt.Name); err == nil && info.Mode()&os.ModeSymlink == 0 && info.IsDir() {
				if err := sw.watcher.Add(evt.Name); err != nil {
					sw.log.Warn(logger.CatApp, "skills hot-reload: cannot watch new skill dir", "path", evt.Name, "err", err.Error())
				}
			}
		}
		if evt.Has(fsnotify.Remove) || evt.Has(fsnotify.Rename) {
			_ = sw.watcher.Remove(evt.Name)
		}
		_ = root
		sw.scheduleRebuild()
		return
	}

	if sw.isWatchedSkillDirEvent(evt.Name) && isSkillEntrypoint(filepath.Base(evt.Name)) {
		sw.scheduleRebuild()
	}
}

func (sw *skillWatcher) skillRootFor(path string) (string, bool) {
	parent := filepath.Clean(filepath.Dir(path))
	for _, root := range sw.dirs {
		if filepath.Clean(root) == parent {
			return root, true
		}
	}
	return "", false
}

func (sw *skillWatcher) isWatchedSkillDirEvent(path string) bool {
	parent := filepath.Clean(filepath.Dir(path))
	for _, root := range sw.dirs {
		root = filepath.Clean(root)
		if filepath.Dir(parent) == root {
			return true
		}
	}
	return false
}

func isSkillEntrypoint(name string) bool {
	switch name {
	case "SKILL.md", "skill.md", "skills.md":
		return true
	default:
		return false
	}
}

func (sw *skillWatcher) scheduleRebuild() {
	select {
	case <-sw.closed:
		return
	default:
	}
	select {
	case sw.rebuildRequest <- struct{}{}:
	default:
	}
}

// Close stops event delivery, cancels pending rebuilds, and waits for the
// watcher goroutine so Stack.Shutdown cannot leave a background listener.
func (sw *skillWatcher) Close() {
	sw.closeOnce.Do(func() {
		close(sw.closed)
		if err := sw.watcher.Close(); err != nil {
			sw.log.Warn(logger.CatApp, "skills hot-reload: failed to close watcher", "err", err.Error())
		}
		<-sw.done
	})
}
