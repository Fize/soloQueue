// Package telemetry provides context helpers and an LLM-client decorator
// for capturing token usage statistics to the shared database.
package telemetry

import "context"

type telemetryContextKey string

const (
	teamIDKey    telemetryContextKey = "team_id"
	usageTypeKey telemetryContextKey = "usage_type"
	metadataKey  telemetryContextKey = "metadata"
)

// Standard Usage Types
const (
	UsageChat       = "chat"
	UsageRouter     = "router"
	UsageCompactor  = "compactor"
	UsageMemory     = "memory"
	UsageSimulation = "simulation"
)

const (
	OriginDesktop    = "desktop"
	OriginPortal     = "portal"
	OriginAPI        = "api"
	OriginQQ         = "qq"
	OriginWechat     = "wechat"
	OriginCron       = "cron"
	OriginWorkflow   = "workflow"
	OriginSimulation = "simulation"
	OriginUnknown    = "unknown"
)

const (
	StatusSuccess   = "success"
	StatusError     = "error"
	StatusCancelled = "cancelled"
	StatusTimeout   = "timeout"
	StatusUnknown   = "unknown"
)

// Metadata carries optional correlation and classification dimensions for one
// LLM call. Missing values are stored as unknown or left empty by design.
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

// WithTelemetryContext injects team and usage type into the context for telemetry tracking.
func WithTelemetryContext(ctx context.Context, teamID string, usageType string) context.Context {
	ctx = context.WithValue(ctx, teamIDKey, teamID)
	ctx = context.WithValue(ctx, usageTypeKey, usageType)
	metadata := MetadataFromContext(ctx)
	metadata.TeamID = teamID
	metadata.UsageType = usageType
	ctx = context.WithValue(ctx, metadataKey, metadata)
	return ctx
}

// WithTelemetryMetadata adds correlation fields while preserving team and
// usage values already attached by WithTelemetryContext.
func WithTelemetryMetadata(ctx context.Context, metadata Metadata) context.Context {
	current := MetadataFromContext(ctx)
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

// MetadataFromContext returns all available telemetry dimensions.
func MetadataFromContext(ctx context.Context) Metadata {
	metadata, _ := ctx.Value(metadataKey).(Metadata)
	if metadata.TeamID == "" {
		metadata.TeamID, _ = ctx.Value(teamIDKey).(string)
	}
	if metadata.UsageType == "" {
		metadata.UsageType, _ = ctx.Value(usageTypeKey).(string)
	}
	return metadata
}

// TelemetryFromContext extracts team and usage type from the context.
func TelemetryFromContext(ctx context.Context) (teamID string, usageType string) {
	metadata := MetadataFromContext(ctx)
	return metadata.TeamID, metadata.UsageType
}
