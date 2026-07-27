package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/approval"
	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/flow"
	"github.com/sourceplane/orun/internal/model"
)

func fastPoll(t *testing.T) {
	t.Helper()
	prev := approvalPollInterval
	approvalPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { approvalPollInterval = prev })
}

// writeGatedWF drops a workflow that leaves a marker file when it runs — the
// observable stand-in for "the engine was invoked" now that the engine is
// in-process.
func writeGatedWF(t *testing.T) (dir, digest, marker string) {
	t.Helper()
	dir = t.TempDir()
	marker = filepath.Join(dir, "ran.txt")
	body := `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: gated }
inputs:
  marker: { type: string, required: true }
steps:
  - name: mark
    run: ["touch", "{{ inputs.marker }}"]
`
	if err := os.WriteFile(filepath.Join(dir, "wf.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, flow.DigestBytes([]byte(body)), marker
}

func gatedStep(digest, marker string) model.PlanStep {
	return model.PlanStep{
		Name: "promote", Workflow: "wf.yaml", WorkflowDigest: digest,
		With:     map[string]interface{}{"marker": marker},
		Approval: &model.StepApproval{Prompt: "ship?", Timeout: "100ms", OnTimeout: "fail"},
	}
}

func workflowRan(marker string) bool {
	_, err := os.Stat(marker)
	return err == nil
}

func TestApprovalTimeoutFailPolicy(t *testing.T) {
	fastPoll(t)
	dir, digest, marker := writeGatedWF(t)
	r := &Runner{ExecID: "exec-a", Stdout: &strings.Builder{}}
	ec := executor.ExecContext{Context: context.Background(), WorkspaceDir: dir}

	_, _, err := r.runWorkflowStep(ec, model.PlanJob{ID: "j"}, gatedStep(digest, marker), false)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("onTimeout=fail must fail the step: %v", err)
	}
	if workflowRan(marker) {
		t.Fatalf("workflow must not run on a failed gate")
	}
}

func TestApprovalTimeoutProceedPolicy(t *testing.T) {
	fastPoll(t)
	dir, digest, marker := writeGatedWF(t)
	r := &Runner{ExecID: "exec-a", Stdout: &strings.Builder{}}
	ec := executor.ExecContext{Context: context.Background(), WorkspaceDir: dir}
	step := gatedStep(digest, marker)
	step.Approval.OnTimeout = "proceed"

	out, _, err := r.runWorkflowStep(ec, model.PlanJob{ID: "j"}, step, false)
	if err != nil {
		t.Fatalf("onTimeout=proceed must run the workflow: %v", err)
	}
	if !workflowRan(marker) {
		t.Fatalf("workflow should have run after policy-proceed")
	}
	if !strings.Contains(out, "policy:onTimeout=proceed") {
		t.Fatalf("the policy verdict must seal into the step output: %q", out)
	}
}

func TestApprovalApprovedRuns(t *testing.T) {
	fastPoll(t)
	dir, digest, marker := writeGatedWF(t)
	r := &Runner{ExecID: "exec-a", Stdout: &strings.Builder{}}
	ec := executor.ExecContext{Context: context.Background(), WorkspaceDir: dir}
	step := gatedStep(digest, marker)
	step.Approval.Timeout = "5s"

	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = approval.Decide(dir, "j", "promote", true, "sam")
	}()
	out, _, err := r.runWorkflowStep(ec, model.PlanJob{ID: "j"}, step, false)
	if err != nil {
		t.Fatalf("approved gate must run: %v", err)
	}
	if !strings.Contains(out, "approved by sam") {
		t.Fatalf("verdict must seal into the output: %q", out)
	}
	if !workflowRan(marker) {
		t.Fatalf("workflow should have run after approval")
	}
}

func TestApprovalRejectedFailsWithoutRunning(t *testing.T) {
	fastPoll(t)
	dir, digest, marker := writeGatedWF(t)
	r := &Runner{ExecID: "exec-a", Stdout: &strings.Builder{}}
	ec := executor.ExecContext{Context: context.Background(), WorkspaceDir: dir}
	step := gatedStep(digest, marker)
	step.Approval.Timeout = "5s"

	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = approval.Decide(dir, "j", "promote", false, "sam")
	}()
	_, _, err := r.runWorkflowStep(ec, model.PlanJob{ID: "j"}, step, false)
	if err == nil || !strings.Contains(err.Error(), "rejected by sam") {
		t.Fatalf("rejected gate must fail the step: %v", err)
	}
	if workflowRan(marker) {
		t.Fatalf("workflow must not run on rejection")
	}
}

func TestApprovalValidation(t *testing.T) {
	if err := (&model.StepApproval{Timeout: "1h", OnTimeout: "fail"}).Validate("s"); err != nil {
		t.Fatalf("valid gate: %v", err)
	}
	if err := (&model.StepApproval{OnTimeout: "fail"}).Validate("s"); err == nil {
		t.Fatalf("missing timeout must fail validation")
	}
	if err := (&model.StepApproval{Timeout: "1h", OnTimeout: "wait"}).Validate("s"); err == nil {
		t.Fatalf("unknown onTimeout must fail validation")
	}
}
