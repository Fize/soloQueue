// Package router classifies work by task type and resolves the configured
// execution model. It intentionally does not rank task difficulty.
package router

import (
	"context"

	"github.com/xiaobaitu/soloqueue/internal/tasktype"
)

type ClassificationSource string

const (
	SourceLocal            ClassificationSource = "local"
	SourceLLM              ClassificationSource = "llm"
	SourceLaya             ClassificationSource = "laya"
	SourceLayaUnavailable  ClassificationSource = "laya_unavailable"
	SourcePreviousFallback ClassificationSource = "previous_fallback"
	SourceDefaultFallback  ClassificationSource = "default_fallback"
)

// Engine names the configured task classifier. The engines are alternatives: with EngineLaya the
// LLM classifier is never called.
type Engine string

const (
	EngineLLM  Engine = "llm"
	EngineLaya Engine = "laya"
)

// Decider is the local decision engine (internal/router/laya.Manager). A nil or failing decider
// degrades to the previous/default fallback, never to a network call.
type Decider interface {
	Decide(ctx context.Context, text string) (tasktype.TaskType, error)
}

// ClassificationResult contains only the information required to choose a
// task model. ReasonCode is diagnostics-only and is never user-facing.
type ClassificationResult struct {
	TaskType   tasktype.TaskType
	Source     ClassificationSource
	ReasonCode string
}

type ClassifierConfig struct {
	EnableLocal bool
	EnableLLM   bool
	Engine      Engine
}

func DefaultClassifierConfig() ClassifierConfig {
	return ClassifierConfig{EnableLocal: true, EnableLLM: true, Engine: EngineLLM}
}

// ClassifyInput separates original user text from execution-only attachment
// annotations. Images are represented only by a boolean for the LLM fallback.
type ClassifyInput struct {
	Text             string
	HasImages        bool
	PreviousTaskType tasktype.TaskType
}
