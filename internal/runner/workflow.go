package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/flow"
	"github.com/sourceplane/orun/internal/model"
)

// runWorkflowStep executes a `workflow:` plan step through the in-process flow
// engine (orun-workflows-v3 WA5 — no external engine, no boundary). It verifies
// the workflow's digest still matches what the plan pinned, loads and runs it,
// and returns a human-readable summary of the run as the step output — sealed
// into .orun/ by the caller via the same AfterStepLog path as any other step
// (§7). A workflow that fails yields a non-nil error so the step is marked
// failed and honors the job's onFailure/retry policy (§8).
func (r *Runner) runWorkflowStep(execCtx executor.ExecContext, job model.PlanJob, step model.PlanStep, resume bool) (string, map[string]any, error) {
	path := r.resolveWorkflowPath(execCtx, step.Workflow)

	// Digest guard (design §5/§7): the plan pinned the file's content digest at
	// compile time; refuse to run a file that changed since — fail-closed.
	if step.WorkflowDigest != "" {
		onDisk, err := flow.Digest(path)
		if err != nil {
			return "", nil, fmt.Errorf("workflow step %q: %w", step.Name, err)
		}
		if onDisk != step.WorkflowDigest {
			return "", nil, fmt.Errorf("workflow %s changed since it was pinned: on-disk %s != pinned %s", path, onDisk, step.WorkflowDigest)
		}
	}

	wf, err := flow.Load(path)
	if err != nil {
		return "", nil, fmt.Errorf("workflow step %q: %w", step.Name, err)
	}

	// The connections grant (design §9): resolve exactly the refs the plan
	// granted, keyed by the workflow's own connection names. The job's wider
	// SecretEnv never crosses into the engine (invariant 10). Values stay
	// in-memory and are masked by the runner's single redaction site.
	connections, err := buildConnectionPayloads(job, step, execCtx.SecretEnv)
	if err != nil {
		return "", nil, err
	}

	// Approval gate (orun-workflows-v2 §9): pause BEFORE running the workflow.
	// The pause and the verdict are sealed run facts; a rejected or
	// fail-on-timeout gate fails the step without the workflow ever running.
	approvalNote, err := r.awaitStepApproval(execCtx, job, step)
	if err != nil {
		return "", nil, err
	}

	// Per-step run root under the workspace's .orun tree (§6): the engine's
	// file-backed run state lands here, so a retry attempt that opted into
	// resume (§8) can re-enter the same run and re-execute only what did not
	// succeed.
	runRoot := r.workflowRunDir(execCtx, job, step)
	execID := ""
	if resume && runRoot != "" {
		execID = latestFlowExecID(runRoot)
	}

	res, err := flow.Run(execCtx.Context, wf, flow.RunOptions{
		Dir:         filepath.Dir(path),
		Inputs:      step.With,
		Connections: connections,
		RunRoot:     runRoot,
		ExecID:      execID,
		Digest:      step.WorkflowDigest,
	})
	if err != nil {
		return "", nil, fmt.Errorf("workflow step %q: %w", step.Name, err)
	}
	output := approvalNote + formatWorkflowResult(step, res)
	if res.Status != "succeeded" {
		return output, nil, fmt.Errorf("workflow step %q failed: %s", step.Name, workflowFailureReason(res))
	}
	return output, res.Outputs, nil
}

// latestFlowExecID finds the most recent flow run recorded under runRoot, so a
// resume attempt re-enters the failed run's state. Empty when none exists — the
// retry then simply runs fresh.
func latestFlowExecID(runRoot string) string {
	entries, err := os.ReadDir(runRoot)
	if err != nil {
		return ""
	}
	best := ""
	var bestMod int64 = -1
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, ierr := os.Stat(filepath.Join(runRoot, e.Name(), "metadata.json"))
		if ierr != nil {
			continue
		}
		if mod := info.ModTime().UnixNano(); mod > bestMod {
			best, bestMod = e.Name(), mod
		}
	}
	return best
}

// workflowFailureReason renders a one-line reason from the failed/blocked steps.
func workflowFailureReason(res *flow.RunResult) string {
	var parts []string
	for _, name := range sortedStepNames(res.Steps) {
		st := res.Steps[name]
		if st.Status == "failed" && st.Error != "" {
			parts = append(parts, fmt.Sprintf("%s: %s", name, st.Error))
		}
	}
	if len(parts) == 0 {
		return "workflow reported status " + res.Status
	}
	return strings.Join(parts, "; ")
}

// substituteWorkflowOutputs resolves ${{ steps.X.outputs.Y }} references in a
// step's executable fields from the outputs earlier workflow steps of this job
// recorded (orun-workflows-v2 §5). The compiler validated the grammar against
// declared names; a reference that still cannot resolve at run time (the
// producing step skipped, or a workflow under-delivered) fails the step closed.
func substituteWorkflowOutputs(step model.PlanStep, outputs map[string]map[string]any) (model.PlanStep, error) {
	lookup := func(stepID, name string) (string, bool) {
		outs, ok := outputs[stepID]
		if !ok {
			return "", false
		}
		value, ok := outs[name]
		if !ok {
			return "", false
		}
		return fmt.Sprint(value), true
	}
	var err error
	if step.Run, err = flow.SubstituteOutputRefs(step.Run, lookup); err != nil {
		return step, fmt.Errorf("step %q: %w", step.Name, err)
	}
	if step.Use, err = flow.SubstituteOutputRefs(step.Use, lookup); err != nil {
		return step, fmt.Errorf("step %q: %w", step.Name, err)
	}
	step.Env, err = substituteInMap(step.Env, lookup)
	if err != nil {
		return step, fmt.Errorf("step %q: %w", step.Name, err)
	}
	step.With, err = substituteInMap(step.With, lookup)
	if err != nil {
		return step, fmt.Errorf("step %q: %w", step.Name, err)
	}
	return step, nil
}

// substituteInMap applies output-reference substitution to every string value
// (one level deep — the shape step env/with carry).
func substituteInMap(m map[string]interface{}, lookup func(string, string) (string, bool)) (map[string]interface{}, error) {
	if len(m) == 0 {
		return m, nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			substituted, err := flow.SubstituteOutputRefs(s, lookup)
			if err != nil {
				return nil, err
			}
			out[k] = substituted
			continue
		}
		out[k] = v
	}
	return out, nil
}

// workflowRunDir provisions the per-step run root the flow engine keeps its
// file-backed run state in (§6): an input to sealing under the workspace's
// .orun tree, never the durable record. Best-effort — an empty string lets the
// engine fall back to its default run root.
func (r *Runner) workflowRunDir(execCtx executor.ExecContext, job model.PlanJob, step model.PlanStep) string {
	base := execCtx.WorkspaceDir
	if base == "" {
		base = execCtx.WorkDir
	}
	if base == "" {
		return ""
	}
	dir := filepath.Join(base, ".orun", "wfruns", r.ExecID, sanitizePathSegment(job.ID), sanitizePathSegment(stepIdentifier(step)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	return dir
}

// sanitizePathSegment keeps job/step identifiers filesystem-safe.
func sanitizePathSegment(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.', r == '@':
			return r
		default:
			return '-'
		}
	}, s)
}

// resolveWorkflowPath resolves a workflow reference to an on-disk path against
// the workspace root (where the intent lives), falling back to the step working
// directory — the same base the compiler pinned the digest against (§5).
func (r *Runner) resolveWorkflowPath(execCtx executor.ExecContext, ref string) string {
	if filepath.IsAbs(ref) {
		return ref
	}
	base := execCtx.WorkspaceDir
	if base == "" {
		base = execCtx.WorkDir
	}
	if base == "" {
		return ref
	}
	return filepath.Join(base, ref)
}

// buildConnectionPayloads materializes the step's compile-checked grant into
// credential payloads: for each granted connection, each field's secret://
// reference is looked up among the job's SecretRefs and resolved from the job's
// already-resolved SecretEnv. Only granted refs are injected — an unmapped
// secret provably never crosses (design §9, invariant 10). A grant referencing
// a secret the job does not carry is a launch error, fail-closed.
func buildConnectionPayloads(job model.PlanJob, step model.PlanStep, secretEnv map[string]string) (map[string]map[string]string, error) {
	if len(step.Connections) == 0 {
		return nil, nil
	}
	refToEnv := make(map[string]string, len(job.SecretRefs))
	for _, sr := range job.SecretRefs {
		refToEnv[sr.Ref] = sr.AsEnv
	}
	out := make(map[string]map[string]string, len(step.Connections))
	for conn, fields := range step.Connections {
		payload := make(map[string]string, len(fields))
		for field, ref := range fields {
			asEnv, ok := refToEnv[ref]
			if !ok {
				return nil, fmt.Errorf("step %q connection %q field %q references %s, which is not among the job's secretRefs — declare it on the job so the resolver can lease it", step.Name, conn, field, ref)
			}
			value, ok := secretEnv[asEnv]
			if !ok {
				return nil, fmt.Errorf("step %q connection %q field %q: secret %s (%s) was not resolved for this job", step.Name, conn, field, ref, asEnv)
			}
			payload[field] = value
		}
		out[conn] = payload
	}
	return out, nil
}

// sortedStepNames returns the run's step names in stable order.
func sortedStepNames(steps map[string]*flow.StepState) []string {
	names := make([]string, 0, len(steps))
	for name := range steps {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// formatWorkflowResult renders a readable summary of a workflow run for the
// step log. Any secret values in the summary are masked by the runner's single
// redaction site before the output reaches any sink.
func formatWorkflowResult(step model.PlanStep, res *flow.RunResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "workflow %s: %s\n", step.Workflow, res.Status)
	for _, name := range sortedStepNames(res.Steps) {
		s := res.Steps[name]
		if s.Error != "" {
			fmt.Fprintf(&b, "  - %s: %s (%s)\n", name, s.Status, s.Error)
		} else {
			fmt.Fprintf(&b, "  - %s: %s\n", name, s.Status)
		}
	}
	if len(res.Outputs) > 0 {
		if out, err := json.MarshalIndent(res.Outputs, "", "  "); err == nil {
			fmt.Fprintf(&b, "outputs:\n%s\n", out)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
