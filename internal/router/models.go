// Package router classifies work by task type and resolves the configured
// execution model. It intentionally does not rank task difficulty.
package router

import "github.com/xiaobaitu/soloqueue/internal/tasktype"

type ClassificationSource string

const (
	SourceLocal            ClassificationSource = "local"
	SourceLLM              ClassificationSource = "llm"
	SourcePreviousFallback ClassificationSource = "previous_fallback"
	SourceDefaultFallback  ClassificationSource = "default_fallback"
)

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
}

func DefaultClassifierConfig() ClassifierConfig {
	return ClassifierConfig{EnableLocal: true, EnableLLM: true}
}

// ClassifyInput separates original user text from execution-only attachment
// annotations. Images are represented only by a boolean for the LLM fallback.
type ClassifyInput struct {
	Text             string
	HasImages        bool
	PreviousTaskType tasktype.TaskType
}
