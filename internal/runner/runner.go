package runner

import (
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/sourceplane/liteci/internal/model"
)

// Runner executes a compiled plan in dependency order.
type Runner struct {
	WorkDir string
	Stdout  io.Writer
	Stderr  io.Writer
	DryRun  bool
}

func NewRunner(workDir string, stdout, stderr io.Writer, dryRun bool) *Runner {
	return &Runner{
		WorkDir: workDir,
		Stdout:  stdout,
		Stderr:  stderr,
		DryRun:  dryRun,
	}
}

func (r *Runner) Run(plan *model.Plan) error {
	if plan == nil {
		return fmt.Errorf("plan cannot be nil")
	}

	orderedJobs, err := topologicalOrder(plan.Jobs)
	if err != nil {
		return err
	}

	for _, job := range orderedJobs {
		fmt.Fprintf(r.Stdout, "→ Job %s (%s/%s)\n", job.ID, job.Component, job.Environment)
		for _, step := range job.Steps {
			fmt.Fprintf(r.Stdout, "  - Step %s\n", step.Name)
			if r.DryRun {
				fmt.Fprintf(r.Stdout, "    %s\n", step.Run)
				continue
			}

			cmd := exec.Command("sh", "-c", step.Run)
			cmd.Dir = r.resolveWorkingDir(job.Path)
			cmd.Stdout = r.Stdout
			cmd.Stderr = r.Stderr

			if err := cmd.Run(); err != nil {
				return fmt.Errorf("job %s step %s failed: %w", job.ID, step.Name, err)
			}
		}
	}

	return nil
}

func (r *Runner) resolveWorkingDir(path string) string {
	if path == "" || path == "./" {
		return r.WorkDir
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(r.WorkDir, path)
}

func topologicalOrder(jobs []model.PlanJob) ([]model.PlanJob, error) {
	jobsByID := make(map[string]model.PlanJob, len(jobs))
	inDegree := make(map[string]int, len(jobs))
	dependents := make(map[string][]string, len(jobs))

	for _, job := range jobs {
		jobsByID[job.ID] = job
		inDegree[job.ID] = 0
		dependents[job.ID] = []string{}
	}

	for _, job := range jobs {
		for _, dep := range job.DependsOn {
			if _, exists := jobsByID[dep]; !exists {
				return nil, fmt.Errorf("job %s depends on unknown job %s", job.ID, dep)
			}
			inDegree[job.ID]++
			dependents[dep] = append(dependents[dep], job.ID)
		}
	}

	queue := make([]string, 0)
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue)

	ordered := make([]model.PlanJob, 0, len(jobs))
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		ordered = append(ordered, jobsByID[current])

		for _, dep := range dependents[current] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
		sort.Strings(queue)
	}

	if len(ordered) != len(jobs) {
		return nil, fmt.Errorf("cycle detected in plan jobs")
	}

	return ordered, nil
}
