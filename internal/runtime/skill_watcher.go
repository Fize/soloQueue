package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/xiaobaitu/soloqueue/internal/agenttools/skill"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

const skillHotReloadDebounce = 250 * time.Millisecond

// skillWatcher watches each directory discovered under an installed-skills
// root. ClawHub creates directories before extracting SKILL.md, so newly
// discoverable directories are added before the debounced rebuild runs.
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
	watchedDirs    map[string]struct{}
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
		watchedDirs:    make(map[string]struct{}),
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
		if err := sw.refreshSkillWatches(dir); err != nil {
			_ = watcher.Close()
			log.Warn(logger.CatApp, "skills hot-reload: cannot enumerate skills dirs", "path", dir, "err", err.Error())
			return nil, err
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

func (sw *skillWatcher) refreshSkillWatches(root string) error {
	discovered, err := skill.DiscoverSkillDirectories(root)
	if err != nil {
		return err
	}
	desired := make(map[string]struct{}, len(discovered))
	for _, path := range discovered {
		path = filepath.Clean(path)
		desired[path] = struct{}{}
		if _, watched := sw.watchedDirs[path]; watched {
			continue
		}
		if err := sw.watcher.Add(path); err != nil {
			return fmt.Errorf("watch skill dir %s: %w", path, err)
		}
		sw.watchedDirs[path] = struct{}{}
	}
	root = filepath.Clean(root)
	for path := range sw.watchedDirs {
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if _, keep := desired[path]; keep {
			continue
		}
		_ = sw.watcher.Remove(path)
		delete(sw.watchedDirs, path)
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
		directoryChange := sw.isDirectoryChange(evt)
		entrypointChange := isSkillEntrypoint(filepath.Base(evt.Name))
		if directoryChange || entrypointChange {
			if err := sw.refreshSkillWatches(root); err != nil {
				sw.log.Warn(logger.CatApp, "skills hot-reload: refresh watches failed", "path", root, "err", err.Error())
			}
		}
		if entrypointChange || directoryChange {
			sw.scheduleRebuild()
		}
	}
}

func (sw *skillWatcher) isDirectoryChange(evt fsnotify.Event) bool {
	if evt.Has(fsnotify.Create) {
		info, err := os.Lstat(evt.Name)
		return err == nil && info.Mode()&os.ModeSymlink == 0 && info.IsDir()
	}
	if evt.Has(fsnotify.Remove) || evt.Has(fsnotify.Rename) {
		_, watched := sw.watchedDirs[filepath.Clean(evt.Name)]
		return watched
	}
	return false
}

func (sw *skillWatcher) skillRootFor(path string) (string, bool) {
	path = filepath.Clean(path)
	best := ""
	for _, root := range sw.dirs {
		root = filepath.Clean(root)
		rel, err := filepath.Rel(root, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if len(root) > len(best) {
				best = root
			}
		}
	}
	return best, best != ""
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
