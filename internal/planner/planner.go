package planner

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/sourceplane/liteci/internal/model"
)

// JobPlanner binds components to jobs and creates instances
type JobPlanner struct {
	compositions    map[string]*CompositionInfo // Composition -> default job info
	templateCache   map[string]*template.Template
}

// CompositionInfo holds the default job for a composition
type CompositionInfo struct {
	Type       string
	DefaultJob *model.JobSpec
}

// NewJobPlanner creates a new job planner from a composition registry
func NewJobPlanner(compositions map[string]*CompositionInfo) *JobPlanner {
	return &JobPlanner{
		compositions:   compositions,
		templateCache:  make(map[string]*template.Template),
	}
}

// PlanJobs creates job instances from component instances
func (jp *JobPlanner) PlanJobs(instances map[string][]*model.ComponentInstance) (map[string]*model.JobInstance, error) {
	jobInstances := make(map[string]*model.JobInstance)

	for envName, envInstances := range instances {
		for _, compInst := range envInstances {
			// Get job definition for this component type
			compositionInfo, exists := jp.compositions[compInst.Type]
			if !exists {
				return nil, fmt.Errorf("no job definition for type: %s", compInst.Type)
			}

			jobDef := compositionInfo.DefaultJob
			if jobDef == nil {
				return nil, fmt.Errorf("no default job defined for type: %s", compInst.Type)
			}

			// Create job instance
			jobID := fmt.Sprintf("%s@%s.%s", compInst.ComponentName, envName, jobDef.Name)
			jobInst := &model.JobInstance{
				ID:          jobID,
				Name:        jobDef.Name,
				Component:   compInst.ComponentName,
				Environment: envName,
				Composition: compInst.Type,
				Path:        compInst.Path,
				Timeout:     jobDef.Timeout,
				Retries:     jobDef.Retries,
				Labels:      compInst.Labels,
				Config:      compInst.Inputs,
				DependsOn:   make([]string, 0),
			}

			// Render steps with template variables
			renderedSteps, err := jp.renderSteps(jobDef.Steps, compInst)
			if err != nil {
				return nil, fmt.Errorf("failed to render steps for job %s: %w", jobID, err)
			}
			jobInst.Steps = renderedSteps

			jobInstances[jobID] = jobInst
		}
	}

	// Resolve job dependencies
	err := jp.resolveDependencies(jobInstances, instances)
	if err != nil {
		return nil, err
	}

	return jobInstances, nil
}
// Templates are cached to avoid re-parsing identical steps across multiple instances
func (jp *JobPlanner) renderSteps(steps []model.Step, compInst *model.ComponentInstance) ([]model.RenderedStep, error) {
	rendered := make([]model.RenderedStep, 0, len(steps))

	// Build template context once
	context := map[string]interface{}{
		"Component":   compInst.ComponentName,
		"Environment": compInst.Environment,
		"Type":        compInst.Type,
	}

	// Add all inputs to context
	for k, v := range compInst.Inputs {
		context[k] = v
	}

	for _, step := range steps {
		// Use cache key: componentType:stepName (steps are unique within a job type)
		cacheKey := fmt.Sprintf("%s:%s", compInst.Type, step.Name)

		// Check cache first
		tmpl, exists := jp.templateCache[cacheKey]
		if !exists {
			// Parse and cache the template
			var err error
			tmpl, err = template.New(cacheKey).Parse(step.Run)
			if err != nil {
				return nil, fmt.Errorf("invalid template in step %s: %w", step.Name, err)
			}
			jp.templateCache[cacheKey] = tmpl
		}

		// Execute the (cached) template
		var buf strings.Builder
		if err := tmpl.Execute(&buf, context); err != nil {
			return nil, fmt.Errorf("failed to execute template in step %s: %w", step.Name, err)
		}

		rendered = append(rendered, model.RenderedStep{
			Name:      step.Name,
			Run:       buf.String(),
			Timeout:   step.Timeout,
			Retry:     step.Retry,
			OnFailure: step.OnFailure,
		})
	}

	return rendered, nil
}

// resolveDependencies sets up dependency edges between job instances
func (jp *JobPlanner) resolveDependencies(jobInstances map[string]*model.JobInstance, compInstances map[string][]*model.ComponentInstance) error {
	// Build a map for fast lookup: (component, environment) -> job IDs
	compToJobs := make(map[string][]string) // key: "comp@env", value: [jobIDs]

	for jobID, job := range jobInstances {
		key := fmt.Sprintf("%s@%s", job.Component, job.Environment)
		compToJobs[key] = append(compToJobs[key], jobID)
	}

	// For each component instance, resolve its dependencies
	for envName, envInstances := range compInstances {
		for _, compInst := range envInstances {
			// Get all jobs for this component
			key := fmt.Sprintf("%s@%s", compInst.ComponentName, envName)
			myJobs, exists := compToJobs[key]
			if !exists {
				continue
			}

			// Resolve each dependency
			for _, dep := range compInst.DependsOn {
				depKey := fmt.Sprintf("%s@%s", dep.ComponentName, dep.Environment)
				depJobs, exists := compToJobs[depKey]
				if !exists {
					return fmt.Errorf("dependency not found: %s depends on %s", key, depKey)
				}

				// Link all my jobs to all dependency jobs
				for _, myJob := range myJobs {
					jobInstances[myJob].DependsOn = append(jobInstances[myJob].DependsOn, depJobs...)
				}
			}
		}
	}

	return nil
}
