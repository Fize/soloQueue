// Package supervised provides one process-owned supervision boundary for LLM clients.
//
// It exists so background callers cannot accidentally bypass RunWatch by using
// context.Background(), while provider implementations remain responsible only
// for protocol and transport details.
package supervised

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/xiaobaitu/soloqueue/internal/agent/llmtypes"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetryctx"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/runwatch"
)

type Client struct {
	inner   llmtypes.LLMClient
	manager *runwatch.Manager
	seq     atomic.Uint64
}

func New(inner llmtypes.LLMClient, manager *runwatch.Manager) *Client {
	return &Client{inner: inner, manager: manager}
}

var _ llmtypes.LLMClient = (*Client)(nil)

type scope struct {
	ctx        context.Context
	ownedModel *runwatch.Handle
	ownedRoot  *runwatch.Handle
	finishOnce sync.Once
}

func (c *Client) begin(ctx context.Context, req llmtypes.LLMRequest) (scope, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c.manager == nil {
		return scope{ctx: ctx}, nil
	}

	parent := runwatch.HandleFromContext(ctx)
	var ownedRoot *runwatch.Handle
	if parent == nil {
		meta := telemetryctx.FromContext(ctx)
		runID := meta.RunID
		if runID == "" {
			runID = "llm-root:" + uuid.NewString()
		}
		ctx = telemetryctx.WithMetadata(ctx, telemetryctx.Metadata{RunID: runID, Origin: telemetryctx.OriginSystem})
		watchCtx, root, err := c.manager.Start(ctx, runwatch.Metadata{
			RunID:          runID,
			OwnerSessionID: meta.SessionID,
			Phase:          "llm",
		})
		if err != nil {
			return scope{}, err
		}
		ctx = watchCtx
		parent = root
		ownedRoot = root
	}

	model := parent
	if parent.Kind() != runwatch.KindModel {
		id := fmt.Sprintf("model:shared:%d", c.seq.Add(1))
		var err error
		model, err = parent.BeginOperation(runwatch.KindModel, id, runwatch.Policy{})
		if err != nil {
			if ownedRoot != nil {
				ownedRoot.Fail(err)
			}
			return scope{}, err
		}
		ctx = runwatch.ContextWithHandle(ctx, model)
	}
	ownedModel := model
	if parent.Kind() == runwatch.KindModel {
		// A model handle supplied by the caller belongs to the caller's
		// supervision tree and must not be completed by this wrapper.
		ownedModel = nil
	}
	return scope{ctx: ctx, ownedModel: ownedModel, ownedRoot: ownedRoot}, nil
}

func (s *scope) finish(err error) {
	s.finishOnce.Do(func() {
		if s.ownedModel != nil {
			if err != nil {
				s.ownedModel.Fail(err)
			} else {
				s.ownedModel.Complete()
			}
		}
		if s.ownedRoot != nil {
			s.ownedRoot.Complete()
		}
	})
}

func (c *Client) Chat(ctx context.Context, req llmtypes.LLMRequest) (*llmtypes.LLMResponse, error) {
	s, err := c.begin(ctx, req)
	if err != nil {
		return nil, err
	}
	resp, err := c.inner.Chat(s.ctx, req)
	s.finish(err)
	return resp, err
}

func (c *Client) ChatStream(ctx context.Context, req llmtypes.LLMRequest) (<-chan llm.Event, error) {
	s, err := c.begin(ctx, req)
	if err != nil {
		return nil, err
	}
	in, err := c.inner.ChatStream(s.ctx, req)
	if err != nil {
		s.finish(err)
		return nil, err
	}
	out := make(chan llm.Event, 64)
	go func() {
		defer close(out)
		var streamErr error
		defer func() { s.finish(streamErr) }()
		for {
			var ev llm.Event
			var ok bool
			select {
			case <-s.ctx.Done():
				streamErr = context.Cause(s.ctx)
				return
			case ev, ok = <-in:
				if !ok {
					return
				}
			}
			if ev.Type == llm.EventError && ev.Err != nil {
				streamErr = ev.Err
			}
			select {
			case out <- ev:
			case <-s.ctx.Done():
				streamErr = context.Cause(s.ctx)
				return
			}
		}
	}()
	return out, nil
}
