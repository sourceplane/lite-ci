package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sourceplane/liteci/internal/model"
)

// PlanViewer provides human-readable visualization of a plan DAG
type PlanViewer struct {
	plan *model.Plan
}

// NewPlanViewer creates a new plan viewer
func NewPlanViewer(plan *model.Plan) *PlanViewer {
	return &PlanViewer{plan: plan}
}

// ViewDAG returns a human-readable tree view of the DAG
func (pv *PlanViewer) ViewDAG() string {
	if len(pv.plan.Jobs) == 0 {
		return "No jobs in plan"
	}

	// Group jobs by component and environment
	componentMap := make(map[string]map[string][]*model.PlanJob)
	for i := range pv.plan.Jobs {
		job := &pv.plan.Jobs[i]
		if componentMap[job.Component] == nil {
			componentMap[job.Component] = make(map[string][]*model.PlanJob)
		}
		componentMap[job.Component][job.Environment] = append(componentMap[job.Component][job.Environment], job)
	}

	// Sort components and environments for consistent output
	components := make([]string, 0, len(componentMap))
	for comp := range componentMap {
		components = append(components, comp)
	}
	sort.Strings(components)

	var sb strings.Builder
	
	// Iterate through components
	for i, component := range components {
		isLastComponent := i == len(components)-1

		// Get composition type from first job of this component
		var compositionType string
		if len(componentMap[component]) > 0 {
			for _, jobs := range componentMap[component] {
				if len(jobs) > 0 {
					compositionType = jobs[0].Composition
					break
				}
			}
		}

		// Component header with composition type in brackets
		componentPrefix := "├─ "
		if isLastComponent {
			componentPrefix = "└─ "
		}
		if compositionType != "" {
			sb.WriteString(fmt.Sprintf("%s%s [%s]\n", componentPrefix, component, compositionType))
		} else {
			sb.WriteString(fmt.Sprintf("%s%s\n", componentPrefix, component))
		}

		// Get sorted environments for this component
		envMap := componentMap[component]
		environments := make([]string, 0, len(envMap))
		for env := range envMap {
			environments = append(environments, env)
		}
		sort.Strings(environments)

		// Iterate through environments
		for j, env := range environments {
			isLastEnv := j == len(environments)-1
			jobs := envMap[env]

			// Sort jobs for consistent output
			sort.Slice(jobs, func(a, b int) bool {
				return jobs[a].Name < jobs[b].Name
			})

			// Environment line
			envPrefix := "│  ├─ "
			envConnector := "│  │"
			if isLastEnv {
				envPrefix = "│  └─ "
				envConnector = "│     "
			}
			if isLastComponent {
				envPrefix = strings.Replace(envPrefix, "│", " ", 1)
				envConnector = strings.Replace(envConnector, "│", " ", 1)
			}

			sb.WriteString(fmt.Sprintf("%s%s\n", envPrefix, env))

			// Iterate through jobs in this environment
			for k, job := range jobs {
				isLastJob := k == len(jobs)-1

				jobPrefix := envConnector + "  ├─ "
				jobConnector := envConnector + "  │"
				if isLastJob {
					jobPrefix = envConnector + "  └─ "
					jobConnector = envConnector + "     "
				}

				// Show job with composition and registry info
				jobLine := fmt.Sprintf("%s%s", jobPrefix, job.Name)
				if job.Timeout != "" {
					jobLine += fmt.Sprintf(" [%s]", job.Timeout)
				}
				if job.Retries > 0 {
					jobLine += fmt.Sprintf(" (retry:%dx)", job.Retries)
				}
				sb.WriteString(jobLine + "\n")

				// Show dependencies if any
				if len(job.DependsOn) > 0 {
					sort.Strings(job.DependsOn)
					for l, dep := range job.DependsOn {
						isLastDep := l == len(job.DependsOn)-1
						depPrefix := jobConnector + "  ├─ "
						if isLastDep {
							depPrefix = jobConnector + "  └─ "
						}
						sb.WriteString(fmt.Sprintf("%s(depends on) %s\n", depPrefix, dep))
					}
				}

				// Show steps
				if len(job.Steps) > 0 {
					for l, step := range job.Steps {
						isLastStep := l == len(job.Steps)-1
						stepPrefix := jobConnector + "  ├─ "
						if isLastStep && len(job.DependsOn) == 0 {
							stepPrefix = jobConnector + "  └─ "
						}
						if len(job.DependsOn) > 0 && isLastStep {
							// After showing deps, use different connector
							stepPrefix = jobConnector + "  ├─ "
						}

						stepLine := fmt.Sprintf("%s%s", stepPrefix, step.Name)
						if step.Run != "" {
							// Truncate long run commands for readability
							runCmd := step.Run
							if len(runCmd) > 60 {
								runCmd = runCmd[:57] + "..."
							}
							stepLine += fmt.Sprintf(" | %s", runCmd)
						}
						sb.WriteString(stepLine + "\n")
					}
				}
			}
		}
		sb.WriteString("\n")
	}

	// Summary
	sb.WriteString("═══════════════════════════════════════════════════════════\n")
	sb.WriteString(fmt.Sprintf("Summary: %d components, %d jobs\n", len(components), len(pv.plan.Jobs)))

	return sb.String()
}

// ViewByComponent shows a component-focused view with all its jobs
func (pv *PlanViewer) ViewByComponent(componentName string) string {
	var matchingJobs []*model.PlanJob
	for i := range pv.plan.Jobs {
		if pv.plan.Jobs[i].Component == componentName {
			matchingJobs = append(matchingJobs, &pv.plan.Jobs[i])
		}
	}

	if len(matchingJobs) == 0 {
		return fmt.Sprintf("No jobs found for component: %s", componentName)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s [%s]\n", componentName, matchingJobs[0].Composition))
	sb.WriteString("═══════════════════════════════════════════════════════════\n\n")

	// Group by environment
	envMap := make(map[string][]*model.PlanJob)
	for _, job := range matchingJobs {
		envMap[job.Environment] = append(envMap[job.Environment], job)
	}

	// Sort environments
	environments := make([]string, 0, len(envMap))
	for env := range envMap {
		environments = append(environments, env)
	}
	sort.Strings(environments)

	// Show each environment
	for _, env := range environments {
		jobs := envMap[env]
		sb.WriteString(fmt.Sprintf("%s (%d jobs)\n", env, len(jobs)))

		sort.Slice(jobs, func(a, b int) bool {
			return jobs[a].Name < jobs[b].Name
		})

		for i, job := range jobs {
			prefix := "├─ "
			if i == len(jobs)-1 {
				prefix = "└─ "
			}
			connector := "│  "
			if i == len(jobs)-1 {
				connector = "   "
			}

			sb.WriteString(fmt.Sprintf("%s%s\n", prefix, job.Name))
			if job.JobRegistry != "" {
				sb.WriteString(fmt.Sprintf("%s  Registry: %s\n", connector, job.JobRegistry))
			}
			if job.Job != "" {
				sb.WriteString(fmt.Sprintf("%s  Job: %s\n", connector, job.Job))
			}
			if job.Timeout != "" {
				sb.WriteString(fmt.Sprintf("%s  Timeout: %s\n", connector, job.Timeout))
			}
			if job.Retries > 0 {
				sb.WriteString(fmt.Sprintf("%s  Retries: %d\n", connector, job.Retries))
			}

			if len(job.DependsOn) > 0 {
				sb.WriteString(fmt.Sprintf("%s  Dependencies:\n", connector))
				for _, dep := range job.DependsOn {
					sb.WriteString(fmt.Sprintf("%s    %s\n", connector, dep))
				}
			}

			if len(job.Steps) > 0 {
				sb.WriteString(fmt.Sprintf("%s  Steps:\n", connector))
				for j, step := range job.Steps {
					stepPrefix := "├─ "
					if j == len(job.Steps)-1 {
						stepPrefix = "└─ "
					}
					sb.WriteString(fmt.Sprintf("%s    %s%s\n", connector, stepPrefix, step.Name))
					if step.Run != "" {
						sb.WriteString(fmt.Sprintf("%s    %s   Run: %s\n", connector, "   ", step.Run))
					}
				}
			}
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// ViewDependencies shows job dependencies in a focused way
func (pv *PlanViewer) ViewDependencies() string {
	if len(pv.plan.Jobs) == 0 {
		return "No jobs in plan"
	}

	var sb strings.Builder
	sb.WriteString("Job Dependencies\n")
	sb.WriteString("═══════════════════════════════════════════════════════════\n\n")

	// Sort jobs by name
	jobs := make([]*model.PlanJob, len(pv.plan.Jobs))
	for i := range pv.plan.Jobs {
		jobs[i] = &pv.plan.Jobs[i]
	}
	sort.Slice(jobs, func(a, b int) bool {
		return jobs[a].ID < jobs[b].ID
	})

	for i, job := range jobs {
		prefix := "├─ "
		if i == len(jobs)-1 {
			prefix = "└─ "
		}

		sb.WriteString(fmt.Sprintf("%s%s (%s/%s)\n", prefix, job.Name, job.Component, job.Environment))

		if len(job.DependsOn) == 0 {
			sb.WriteString("   (no dependencies)\n")
		} else {
			for j, dep := range job.DependsOn {
				depPrefix := "  ├─ "
				if j == len(job.DependsOn)-1 {
					depPrefix = "  └─ "
				}
				sb.WriteString(fmt.Sprintf("%s(depends on) %s\n", depPrefix, dep))
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
