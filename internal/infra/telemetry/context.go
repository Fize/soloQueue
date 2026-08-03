// Package telemetry provides context helpers and an LLM-client decorator
// for capturing token usage statistics to the shared database.
package telemetry

import "context"

type telemetryContextKey string

const (
	teamIDKey    telemetryContextKey = "team_id"
	usageTypeKey telemetryContextKey = "usage_type"
)

// Standard Usage Types
const (
	UsageChat       = "chat"
	UsageRouter     = "router"
	UsageCompactor  = "compactor"
	UsageMemory     = "memory"
	UsageSimulation = "simulation"
)

// WithTelemetryContext injects team and usage type into the context for telemetry tracking.
func WithTelemetryContext(ctx context.Context, teamID string, usageType string) context.Context {
	ctx = context.WithValue(ctx, teamIDKey, teamID)
	ctx = context.WithValue(ctx, usageTypeKey, usageType)
	return ctx
}

// TelemetryFromContext extracts team and usage type from the context.
func TelemetryFromContext(ctx context.Context) (teamID string, usageType string) {
	if t, ok := ctx.Value(teamIDKey).(string); ok {
		teamID = t
	}
	if u, ok := ctx.Value(usageTypeKey).(string); ok {
		usageType = u
	}
	return teamID, usageType
}
