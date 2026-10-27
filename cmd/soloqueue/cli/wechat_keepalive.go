package cli

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

const wechatKeepaliveInterval = 6 * time.Hour

type wechatKeepalive struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	routes map[string]*wechatKeepaliveRoute
}

type wechatKeepaliveRoute struct {
	msg        channel.Message
	sources    []wechatKeepaliveSource
	activity   time.Time
	checked    time.Time
	retryAt    time.Time
	failures   int
	generation uint64
	pending    chan struct{}
	cancel     context.CancelFunc
}

type wechatKeepaliveSource struct {
	msg     channel.Message
	binding channel.NotificationBinding
	data    []byte
	save    func([]byte, []byte) (bool, error)
}

func wechatRouteKey(msg channel.Message) string {
	return msg.AccountID + "\x00" + msg.ConversationID + "\x00" + msg.UserID
}

func (m *WechatBotManager) resetWechatKeepalive() {
	m.keepaliveMu.Lock()
	defer m.keepaliveMu.Unlock()
	if m.keepalive != nil {
		m.keepalive.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.keepalive = &wechatKeepalive{ctx: ctx, cancel: cancel, routes: make(map[string]*wechatKeepaliveRoute)}
}

func (m *WechatBotManager) stopWechatKeepalive() {
	m.keepaliveMu.Lock()
	defer m.keepaliveMu.Unlock()
	if m.keepalive != nil {
		m.keepalive.cancel()
		m.keepalive = nil
	}
}

func (m *WechatBotManager) wechatKeeper() *wechatKeepalive {
	m.keepaliveMu.Lock()
	defer m.keepaliveMu.Unlock()
	return m.keepalive
}

func (m *WechatBotManager) runWechatKeepalive(k *wechatKeepalive) {
	if k == nil {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if k.ctx.Err() != nil {
			return
		}
		now := time.Now()
		for _, key := range m.dueWechatKeepaliveRoutes(k, now) {
			go m.refreshWechatContext(k.ctx, k, key, now)
		}
		select {
		case <-k.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Reconciliation reads current notification settings rather than assuming that
// an enabled WeChat account is also selected for notifications.
func (m *WechatBotManager) currentWechatKeepaliveSources(ctx context.Context) []wechatKeepaliveSource {
	var sources []wechatKeepaliveSource
	if m.mgr != nil {
		if sess := m.mgr.Session(); sess != nil {
			data, binding, err := sess.NotificationRoute("wechat")
			if err == nil && len(data) > 0 {
				sources = append(sources, wechatKeepaliveSource{data: data, binding: binding, save: func(before, after []byte) (bool, error) { return sess.UpdateNotificationRoute("wechat", before, after) }})
			}
		}
	}
	if m.l2Store != nil && m.rt != nil && m.rt.TeamStore != nil {
		// TeamStore is authoritative, including leaders with no active session.
		agents, err := m.rt.TeamStore.ListAgents(ctx)
		if err == nil {
			for _, a := range agents {
				if !a.IsLeader || a.NotifyChannel != "wechat" {
					continue
				}
				group := a.TeamName
				data, err := m.l2Store.GroupNotificationRoute(group, "wechat")
				if err == nil && len(data) > 0 {
					sources = append(sources, wechatKeepaliveSource{data: data, binding: channel.NotificationBinding{AccountID: a.Channels["wechat"], Group: group}, save: func(before, after []byte) (bool, error) {
						return m.l2Store.UpdateGroupNotificationRoute(group, "wechat", before, after)
					}})
				}
			}
		}
	}
	valid := sources[:0]
	for _, source := range sources {
		bound := channel.ContextWithNotificationBinding(ctx, source.binding)
		msg, _, err := m.notificationClient(bound, source.data)
		if err == nil {
			source.msg = msg
			valid = append(valid, source)
		}
	}
	return valid
}

func (m *WechatBotManager) selectWechatKeepaliveSources(k *wechatKeepalive, sources []wechatKeepaliveSource) []string {
	selected := make(map[string][]wechatKeepaliveSource)
	for _, source := range sources {
		key := wechatRouteKey(source.msg)
		selected[key] = append(selected[key], source)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.ctx.Err() != nil {
		return nil
	}
	for key, state := range k.routes {
		if _, ok := selected[key]; !ok {
			if state.cancel != nil {
				state.cancel()
			}
			delete(k.routes, key)
		}
	}
	keys := make([]string, 0, len(selected))
	for key, items := range selected {
		state := k.routes[key]
		if state == nil {
			state = &wechatKeepaliveRoute{msg: items[0].msg}
			k.routes[key] = state
		}
		state.sources = items
		keys = append(keys, key)
	}
	return keys
}

// dueWechatKeepaliveRoutes reconciles current selection and filters scheduling
// before the loop allocates workers; sends use the same due predicate.
func (m *WechatBotManager) dueWechatKeepaliveRoutes(k *wechatKeepalive, now time.Time) []string {
	if k == nil || k.ctx.Err() != nil {
		return nil
	}
	keys := m.selectWechatKeepaliveSources(k, m.currentWechatKeepaliveSources(k.ctx))
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.ctx.Err() != nil {
		return nil
	}
	due := keys[:0]
	for _, key := range keys {
		if k.routes[key].due(now) {
			due = append(due, key)
		}
	}
	return due
}

func (state *wechatKeepaliveRoute) due(now time.Time) bool {
	return state != nil && len(state.sources) > 0 && state.pending == nil &&
		!now.Before(state.retryAt) &&
		(state.activity.IsZero() || now.Sub(state.activity) >= wechatKeepaliveInterval) &&
		(state.checked.IsZero() || now.Sub(state.checked) >= wechatKeepaliveInterval)
}

func (m *WechatBotManager) rememberWechatInbound(msg channel.Message) {
	k := m.wechatKeeper()
	if k == nil || msg.UserID == "" || msg.ReplyToken == "" {
		return
	}
	// Unauthorised inbound messages must not affect a saved notification route.
	m.mu.Lock()
	account, ok := m.accounts[msg.AccountID]
	m.mu.Unlock()
	if !ok || !wechatRecipientAllowed(account, msg.UserID) {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.ctx.Err() != nil {
		return
	}
	key := wechatRouteKey(msg)
	state := k.routes[key]
	if state == nil {
		state = &wechatKeepaliveRoute{}
		k.routes[key] = state
	}
	state.msg = channel.Message{Channel: "wechat", AccountID: msg.AccountID, ConversationID: msg.ConversationID, UserID: msg.UserID, ReplyToken: msg.ReplyToken}
	state.activity = time.Now()
	state.generation++
	state.failures = 0
	state.retryAt = time.Time{}
}

func wechatRecipientAllowed(account config.WechatBotConfig, user string) bool {
	if !account.Enabled {
		return false
	}
	if !account.WhitelistEnabled {
		return true
	}
	for _, allowed := range account.Whitelist {
		if allowed == user {
			return true
		}
	}
	return false
}

func (m *WechatBotManager) prepareWechatNotification(ctx context.Context, msg channel.Message) channel.Message {
	if _, ok := channel.NotificationBindingFromContext(ctx); !ok {
		return msg
	}
	k := m.wechatKeeper()
	if k == nil {
		return msg
	}
	m.selectWechatKeepaliveSources(k, m.currentWechatKeepaliveSources(ctx))
	return m.refreshWechatContext(ctx, k, wechatRouteKey(msg), time.Now(), msg)
}

// The optional fallback is the immutable reply route captured by an older live
// sender. A selected route's current context always takes precedence over it.
func (m *WechatBotManager) refreshWechatContext(ctx context.Context, k *wechatKeepalive, key string, now time.Time, fallback ...channel.Message) channel.Message {
	var original channel.Message
	if len(fallback) > 0 {
		original = fallback[0]
	}
	k.mu.Lock()
	state := k.routes[key]
	if state == nil || len(state.sources) == 0 || k.ctx.Err() != nil {
		k.mu.Unlock()
		return original
	}
	current := state.msg
	if state.pending != nil {
		done := state.pending
		k.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
		case <-k.ctx.Done():
		}
		k.mu.Lock()
		if state = k.routes[key]; state != nil {
			current = state.msg
		}
		k.mu.Unlock()
		return current
	}
	if !state.due(now) {
		k.mu.Unlock()
		return current
	}
	requestCtx, cancel := context.WithCancel(k.ctx)
	stop := context.AfterFunc(ctx, cancel)
	state.cancel = cancel
	done := make(chan struct{})
	state.pending = done
	generation := state.generation
	// Bind this response to the metadata observed when its request began.
	// Reconciliation may replace the active source list while the request waits.
	sources := append([]wechatKeepaliveSource(nil), state.sources...)
	for i := range sources {
		sources[i].data = append([]byte(nil), sources[i].data...)
	}
	source := sources[0]
	k.mu.Unlock()
	bound := channel.ContextWithNotificationBinding(requestCtx, source.binding)
	raw, _ := json.Marshal(current)
	_, client, err := m.notificationClient(bound, raw)
	var token string
	if err == nil {
		response, requestErr := client.GetConfig(bound, current.UserID, current.ReplyToken)
		err = requestErr
		token = response.ContextToken
	}
	stop()
	cancel()
	// Recheck config after the request; a switch away must not publish its response.
	selected := false
	for _, candidate := range m.currentWechatKeepaliveSources(ctx) {
		if wechatRouteKey(candidate.msg) == key {
			selected = true
			break
		}
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	defer close(done)
	if k.routes[key] == state && state.pending == done {
		state.pending = nil
		state.cancel = nil
	}
	if k.routes[key] != state || state.generation != generation || k.ctx.Err() != nil || !selected {
		if fresh := k.routes[key]; fresh != nil {
			return fresh.msg
		}
		return current
	}
	if err != nil {
		state.failures++
		delay := time.Minute * time.Duration(1<<min(state.failures-1, 6))
		state.retryAt = now.Add(delay)
		m.mainLog.Warn(logger.CatApp, "wechat notification keepalive failed", "err", err.Error())
		return state.msg
	}
	if token != "" && token != state.msg.ReplyToken {
		renewed := state.msg
		renewed.ReplyToken = token
		after, _ := json.Marshal(renewed)
		updatedAll := true
		for _, saved := range sources {
			updated, saveErr := saved.save(saved.data, after)
			if saveErr != nil {
				m.mainLog.Warn(logger.CatApp, "wechat notification context save failed", "err", saveErr.Error())
			}
			updatedAll = updatedAll && updated && saveErr == nil
		}
		if updatedAll {
			state.msg = renewed
		} else {
			// A new inbound write can win even if its observer has not run yet.
			// Reload the opaque route through its owner instead of publishing the
			// result of a request made with superseded metadata.
			var latest []wechatKeepaliveSource
			for _, candidate := range m.currentWechatKeepaliveSources(ctx) {
				if wechatRouteKey(candidate.msg) == key {
					latest = append(latest, candidate)
				}
			}
			if len(latest) == 0 {
				return state.msg
			}
			state.sources = latest
			state.msg = latest[0].msg
			for _, candidate := range latest {
				if candidate.msg.ReplyToken != renewed.ReplyToken {
					state.msg = candidate.msg
					break
				}
			}
			if state.msg.ReplyToken != renewed.ReplyToken {
				state.generation++
				return state.msg
			}
		}
	}
	state.checked = now
	state.failures = 0
	state.retryAt = time.Time{}
	m.mainLog.Info(logger.CatApp, "wechat notification keepalive accepted")
	return state.msg
}
