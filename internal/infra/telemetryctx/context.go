// Package telemetryctx carries request metadata without depending on the LLM
// client stack, allowing entrypoints such as Cron to annotate contexts safely.
package telemetryctx

import "context"

type contextKey string

const (
	teamIDKey    contextKey = "team_id"
	usageTypeKey contextKey = "usage_type"
	metadataKey  contextKey = "metadata"
)

const (
	UsageChat       = "chat"
	UsageRouter     = "router"
	UsageCompactor  = "compactor"
	UsageMemory     = "memory"
	UsageSimulation = "simulation"
)

const (
	OriginDesktop    = "desktop"
	OriginAPI        = "api"
	OriginQQ         = "qq"
	OriginWechat     = "wechat"
	OriginCron       = "cron"
	OriginSimulation = "simulation"
	OriginSystem     = "system"
)

type Metadata struct {
	RequestID string
	SessionID string
	RunID     string
	AgentID   string
	TeamID    string
	Origin    string
	UsageType string
	TaskType  string
}

func WithContext(ctx context.Context, teamID, usageType string) context.Context {
	ctx = context.WithValue(ctx, teamIDKey, teamID)
	ctx = context.WithValue(ctx, usageTypeKey, usageType)
	metadata := FromContext(ctx)
	metadata.TeamID = teamID
	metadata.UsageType = usageType
	return context.WithValue(ctx, metadataKey, metadata)
}

func WithMetadata(ctx context.Context, metadata Metadata) context.Context {
	current := FromContext(ctx)
	if metadata.RequestID != "" {
		current.RequestID = metadata.RequestID
	}
	if metadata.SessionID != "" {
		current.SessionID = metadata.SessionID
	}
	if metadata.RunID != "" {
		current.RunID = metadata.RunID
	}
	if metadata.AgentID != "" {
		current.AgentID = metadata.AgentID
	}
	if metadata.TeamID != "" {
		current.TeamID = metadata.TeamID
	}
	if metadata.Origin != "" {
		current.Origin = metadata.Origin
	}
	if metadata.UsageType != "" {
		current.UsageType = metadata.UsageType
	}
	if metadata.TaskType != "" {
		current.TaskType = metadata.TaskType
	}
	return context.WithValue(ctx, metadataKey, current)
}

func FromContext(ctx context.Context) Metadata {
	metadata, _ := ctx.Value(metadataKey).(Metadata)
	if metadata.TeamID == "" {
		metadata.TeamID, _ = ctx.Value(teamIDKey).(string)
	}
	if metadata.UsageType == "" {
		metadata.UsageType, _ = ctx.Value(usageTypeKey).(string)
	}
	return metadata
}
