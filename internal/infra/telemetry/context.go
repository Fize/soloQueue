// Package telemetry provides context helpers and an LLM-client decorator
// for capturing token usage statistics to the shared database.
package telemetry

import (
	"context"

	"github.com/xiaobaitu/soloqueue/internal/infra/telemetryctx"
)

type Metadata = telemetryctx.Metadata

const (
	UsageChat       = telemetryctx.UsageChat
	UsageRouter     = telemetryctx.UsageRouter
	UsageCompactor  = telemetryctx.UsageCompactor
	UsageMemory     = telemetryctx.UsageMemory
	UsageSimulation = telemetryctx.UsageSimulation

	OriginDesktop    = telemetryctx.OriginDesktop
	OriginAPI        = telemetryctx.OriginAPI
	OriginQQ         = telemetryctx.OriginQQ
	OriginWechat     = telemetryctx.OriginWechat
	OriginCron       = telemetryctx.OriginCron
	OriginSimulation = telemetryctx.OriginSimulation
	OriginSystem     = telemetryctx.OriginSystem

	StatusSuccess   = "success"
	StatusError     = "error"
	StatusCancelled = "cancelled"
	StatusTimeout   = "timeout"
	StatusUnknown   = "unknown"
)

func WithTelemetryContext(ctx context.Context, teamID, usageType string) context.Context {
	return telemetryctx.WithContext(ctx, teamID, usageType)
}

func WithTelemetryMetadata(ctx context.Context, metadata Metadata) context.Context {
	return telemetryctx.WithMetadata(ctx, metadata)
}

func MetadataFromContext(ctx context.Context) Metadata {
	return telemetryctx.FromContext(ctx)
}

func TelemetryFromContext(ctx context.Context) (teamID, usageType string) {
	metadata := telemetryctx.FromContext(ctx)
	return metadata.TeamID, metadata.UsageType
}
