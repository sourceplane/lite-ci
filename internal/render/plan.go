package render

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sourceplane/liteci/internal/model"
	"gopkg.in/yaml.v3"
)

// Renderer materializes job instances into a Plan
type Renderer struct{}

// NewRenderer creates a new renderer
func NewRenderer() *Renderer {
	return &Renderer{}
}

// RenderPlan creates a plan from job instances with JobRegistry bindings
func (r *Renderer) RenderPlan(metadata model.Metadata, jobInstances map[string]*model.JobInstance, jobBindings map[string]string) *model.Plan {
	plan := &model.Plan{
		APIVersion: "sourceplane.io/v1",
		Kind:       "Workflow",
		Metadata: model.Metadata{
			Name:        metadata.Name,
			Description: metadata.Description,
		},
		Spec: model.PlanSpec{
			JobBindings: jobBindings, // Map of model -> JobRegistry name
		},
		Jobs: make([]model.PlanJob, 0),
	}

	// Convert job instances to plan jobs
	for _, job := range jobInstances {
		// Look up JobRegistry name from bindings
		registryName := ""
		if bindings, ok := jobBindings[job.Composition]; ok {
			registryName = bindings
		}

		planJob := model.PlanJob{
			ID:          job.ID,
			Name:        job.Name,
			Component:   job.Component,
			Environment: job.Environment,
			Composition: job.Composition,
			JobRegistry: registryName,
			Job:         job.Name, // The specific job name from the registry
			Path:        job.Path,
			Steps:       r.convertSteps(job.Steps),
			DependsOn:   job.DependsOn,
			Timeout:     job.Timeout,
			Retries:     job.Retries,
			Env:         job.Config, // Single source: Config
			Labels:      job.Labels,
			Config:      job.Config,
		}

		plan.Jobs = append(plan.Jobs, planJob)
	}

	return plan
}

// convertSteps converts rendered steps to plan steps
func (r *Renderer) convertSteps(steps []model.RenderedStep) []model.PlanStep {
	planSteps := make([]model.PlanStep, len(steps))
	for i, step := range steps {
		planSteps[i] = model.PlanStep{
			Name:      step.Name,
			Run:       step.Run,
			Timeout:   step.Timeout,
			Retry:     step.Retry,
			OnFailure: step.OnFailure,
		}
	}
	return planSteps
}

// RenderJSON renders plan as JSON
func (r *Renderer) RenderJSON(plan *model.Plan) ([]byte, error) {
	return json.MarshalIndent(plan, "", "  ")
}

// RenderYAML renders plan as YAML
func (r *Renderer) RenderYAML(plan *model.Plan) ([]byte, error) {
	return yaml.Marshal(plan)
}

// WritePlan writes plan to file (JSON or YAML based on extension)
func (r *Renderer) WritePlan(plan *model.Plan, path string) error {
	var data []byte
	var err error

	// Ensure directory exists
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}

	// Determine format from extension
	ext := filepath.Ext(path)
	switch ext {
	case ".json":
		data, err = r.RenderJSON(plan)
	case ".yaml", ".yml":
		data, err = r.RenderYAML(plan)
	default:
		// Default to JSON if no extension
		data, err = r.RenderJSON(plan)
	}

	if err != nil {
		return fmt.Errorf("failed to render plan: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write plan to %s: %w", path, err)
	}

	return nil
}

// DebugDump outputs debug information about the plan
func (r *Renderer) DebugDump(plan *model.Plan) string {
	output := fmt.Sprintf("Plan: %s (%s)\n", plan.Metadata.Name, plan.Metadata.Description)
	output += fmt.Sprintf("Jobs: %d\n\n", len(plan.Jobs))

	for _, job := range plan.Jobs {
		output += fmt.Sprintf("Job: %s\n", job.ID)
		output += fmt.Sprintf("  Component: %s\n", job.Component)
		output += fmt.Sprintf("  Environment: %s\n", job.Environment)
		output += fmt.Sprintf("  Composition: %s\n", job.Composition)
		output += fmt.Sprintf("  Steps: %d\n", len(job.Steps))
		output += fmt.Sprintf("  DependsOn: %v\n", job.DependsOn)
		output += "\n"
	}

	return output
}
