package wechat

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

var _ channel.ResponseActivityStarter = (*Client)(nil)

type responseActivity struct {
	client       *Client
	parentCtx    context.Context
	ctx          context.Context
	cancel       context.CancelFunc
	done         chan struct{}
	stopOnce     sync.Once
	userID       string
	typingTicket string
	startedAt    time.Time
}

func (c *Client) StartResponseActivity(ctx context.Context, msg channel.Message) (func(), error) {
	if strings.TrimSpace(msg.UserID) == "" || strings.TrimSpace(msg.ReplyToken) == "" {
		return nil, fmt.Errorf("wechat response activity requires user id and context token")
	}
	startedAt := time.Now()
	config, err := c.GetConfig(ctx, msg.UserID, msg.ReplyToken)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(config.TypingTicket) == "" {
		return nil, fmt.Errorf("wechat getconfig returned an empty typing ticket")
	}

	activityCtx, cancel := context.WithCancel(ctx)
	if err := c.SendTyping(activityCtx, msg.UserID, config.TypingTicket, Typing); err != nil {
		cancel()
		return nil, err
	}

	activity := &responseActivity{
		client:       c,
		parentCtx:    ctx,
		ctx:          activityCtx,
		cancel:       cancel,
		done:         make(chan struct{}),
		userID:       msg.UserID,
		typingTicket: config.TypingTicket,
		startedAt:    startedAt,
	}
	if c.log != nil {
		c.log.InfoContext(ctx, logger.CatApp, "wechat response activity started", "interval_ms", c.typingInterval.Milliseconds())
	}
	go activity.run()
	return activity.stop, nil
}

func (a *responseActivity) run() {
	defer close(a.done)
	ticker := time.NewTicker(a.client.typingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if err := a.client.SendTyping(a.ctx, a.userID, a.typingTicket, Typing); err != nil && a.ctx.Err() == nil && a.client.log != nil {
				a.client.log.WarnContext(a.ctx, logger.CatApp, "wechat typing keepalive failed", "reply_age_ms", time.Since(a.startedAt).Milliseconds(), "err", err.Error())
			}
		}
	}
}

func (a *responseActivity) stop() {
	a.stopOnce.Do(func() {
		a.cancel()
		<-a.done
		if a.client.log != nil {
			a.client.log.InfoContext(context.WithoutCancel(a.parentCtx), logger.CatApp, "wechat response activity stopped", "reply_age_ms", time.Since(a.startedAt).Milliseconds())
		}

		// Cancelling the visible typing indicator is cleanup and must not delay
		// the final reply, especially near the context-token validity boundary.
		go func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(a.parentCtx), a.client.typingCancelTimeout)
			defer cancel()
			if err := a.client.SendTyping(ctx, a.userID, a.typingTicket, TypingStopped); err != nil && a.client.log != nil {
				a.client.log.WarnContext(ctx, logger.CatApp, "wechat typing stop failed", "reply_age_ms", time.Since(a.startedAt).Milliseconds(), "err", err.Error())
			}
		}()
	})
}
