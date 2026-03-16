package pipeline

import (
	"context"
)

// Stage represents a single stage in the task execution pipeline.
type Stage interface {
	Name() string
	Execute(ctx context.Context, task *PipelineTask) error
}

// PipelineTask holds the context for a task moving through the pipeline.
type PipelineTask struct {
	TaskID       string
	AttemptID    int64
	ContainerID  string
	WorkDir      string
	Status       string
	Artifacts    []string
	ValidationOK bool
	ReviewOK     bool
}

// Pipeline orchestrates the sequential execution of stages for a task.
type Pipeline struct {
	stages []Stage
}

// New creates a new Pipeline with the given stages.
func New(stages ...Stage) *Pipeline {
	return &Pipeline{
		stages: stages,
	}
}

// Execute runs all stages in sequence for the given task.
func (p *Pipeline) Execute(ctx context.Context, task *PipelineTask) error {
	return nil
}

// AddStage appends a stage to the pipeline.
func (p *Pipeline) AddStage(stage Stage) {
	p.stages = append(p.stages, stage)
}
