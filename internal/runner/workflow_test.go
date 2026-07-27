package runner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/flow"
	"github.com/sourceplane/orun/internal/model"
)

// writeWorkflow drops a workflow file into a fresh workspace dir and returns
// the dir and the file's content digest (the pin a plan would carry).
func writeWorkflow(t *testing.T, name, body string) (dir, digest string) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, flow.DigestBytes([]byte(body))
}

// writeWF is the trivial always-succeeds fixture shared with the approval tests.
func writeWF(t *testing.T) (dir, digest string) {
	t.Helper()
	return writeWorkflow(t, "wf.yaml", `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: trivial }
steps:
  - name: ok
    run: ["true"]
`)
}

func TestRunWorkflowStepSuccess(t *testing.T) {
	dir, digest := writeWorkflow(t, "wf.yaml", `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: notify }
outputs:
  prUrl: "steps.notify.outputs.v"
steps:
  - name: notify
    action: script
    with: { source: "'https://example/pr/1'" }
    outputs: { v: "output.value" }
`)
	r := &Runner{}
	ec := executor.ExecContext{Context: context.Background(), WorkspaceDir: dir}
	step := model.PlanStep{Name: "s", Workflow: "wf.yaml", WorkflowDigest: digest}

	out, outputs, err := r.runWorkflowStep(ec, model.PlanJob{ID: "web@prod.deploy"}, step, false)
	if err != nil {
		t.Fatalf("runWorkflowStep: %v", err)
	}
	if !strings.Contains(out, "succeeded") || !strings.Contains(out, "notify") {
		t.Fatalf("summary missing status/steps: %q", out)
	}
	// The workflow's declared outputs come back for later steps to consume.
	if outputs["prUrl"] != "https://example/pr/1" {
		t.Fatalf("workflow outputs not returned: %#v", outputs)
	}
}

func TestRunWorkflowStepFailureIsError(t *testing.T) {
	dir, digest := writeWorkflow(t, "wf.yaml", `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: boom }
steps:
  - name: explode
    run: ["false"]
`)
	r := &Runner{}
	ec := executor.ExecContext{Context: context.Background(), WorkspaceDir: dir}
	step := model.PlanStep{Name: "s", Workflow: "wf.yaml", WorkflowDigest: digest}

	out, _, err := r.runWorkflowStep(ec, model.PlanJob{ID: "j"}, step, false)
	if err == nil {
		t.Fatalf("a failed workflow step must return an error")
	}
	if !strings.Contains(err.Error(), "explode") {
		t.Fatalf("the failing workflow step should be named in the error: %v", err)
	}
	if !strings.Contains(out, "failed") {
		t.Fatalf("expected the failure summary to be returned as output, got %q", out)
	}
}

func TestRunWorkflowStepDigestMismatchIsError(t *testing.T) {
	dir, _ := writeWF(t)
	r := &Runner{}
	ec := executor.ExecContext{Context: context.Background(), WorkspaceDir: dir}
	// Pin a stale digest: the on-disk file no longer matches.
	step := model.PlanStep{Name: "s", Workflow: "wf.yaml", WorkflowDigest: "sha256:stale"}

	_, _, err := r.runWorkflowStep(ec, model.PlanJob{ID: "j"}, step, false)
	if err == nil || !strings.Contains(err.Error(), "pinned") {
		t.Fatalf("expected a fail-closed error when the pinned digest is stale, got %v", err)
	}
}

func TestRunWorkflowStepInvalidWorkflowIsError(t *testing.T) {
	// torkflow/v1 is rejected outright at the runner too (design §12).
	body := "apiVersion: torkflow/v1\nkind: Workflow\n"
	dir, digest := writeWorkflow(t, "wf.yaml", body)
	r := &Runner{}
	ec := executor.ExecContext{Context: context.Background(), WorkspaceDir: dir}
	step := model.PlanStep{Name: "s", Workflow: "wf.yaml", WorkflowDigest: digest}

	if _, _, err := r.runWorkflowStep(ec, model.PlanJob{ID: "j"}, step, false); err == nil {
		t.Fatalf("expected an error for a workflow file the engine rejects")
	}
}

func TestRunWorkflowStepGrantInjectsMappedOnly(t *testing.T) {
	// The step's grant materializes the workflow's declared connection; the
	// job's wider SecretEnv never crosses. The credential provably reaches the
	// action (a real HTTP call carries it as the bearer token) and nothing else
	// does.
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotAuth = req.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir, digest := writeWorkflow(t, "wf.yaml", `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: grant }
inputs:
  url: { type: string, required: true }
connections:
  vcs-app: { type: http.bearer }
steps:
  - name: call
    action: http.request
    connection: vcs-app
    with: { url: "{{ inputs.url }}" }
`)
	r := &Runner{}
	const tokenRef = "secret://acme/api/prod/GITHUB_TOKEN"
	job := model.PlanJob{
		ID: "j",
		SecretRefs: []model.PlanSecretRef{
			{AsEnv: "GITHUB_TOKEN", Ref: tokenRef},
			{AsEnv: "UNRELATED", Ref: "secret://acme/api/prod/UNRELATED"},
		},
	}
	ec := executor.ExecContext{
		Context:      context.Background(),
		WorkspaceDir: dir,
		SecretEnv: map[string]string{
			"GITHUB_TOKEN": "ghp_s3cr3t",
			"UNRELATED":    "canary-value", // resolved for the job, NOT granted
		},
	}
	step := model.PlanStep{
		Name: "open-pr", Workflow: "wf.yaml", WorkflowDigest: digest,
		With: map[string]interface{}{"url": srv.URL},
		Connections: map[string]map[string]string{
			"vcs-app": {"bearerToken": tokenRef},
		},
	}

	if _, _, err := r.runWorkflowStep(ec, job, step, false); err != nil {
		t.Fatalf("runWorkflowStep: %v", err)
	}
	if gotAuth != "Bearer ghp_s3cr3t" {
		t.Fatalf("granted credential not injected under its connection name: %q", gotAuth)
	}
	// Invariant 10: the unmapped secret provably does not cross — the payload
	// map handed to the engine holds exactly the granted refs.
	conns, err := buildConnectionPayloads(job, step, ec.SecretEnv)
	if err != nil {
		t.Fatal(err)
	}
	if len(conns) != 1 || len(conns["vcs-app"]) != 1 || conns["vcs-app"]["bearerToken"] != "ghp_s3cr3t" {
		t.Fatalf("payloads must hold exactly the granted refs: %#v", conns)
	}
}

func TestRunWorkflowStepStaleGrantFailsClosed(t *testing.T) {
	// A grant naming a connection the workflow does not declare is refused by
	// the engine before anything runs.
	dir, digest := writeWF(t)
	r := &Runner{}
	job := model.PlanJob{
		ID:         "j",
		SecretRefs: []model.PlanSecretRef{{AsEnv: "T", Ref: "secret://a/b/c/T"}},
	}
	ec := executor.ExecContext{
		Context: context.Background(), WorkspaceDir: dir,
		SecretEnv: map[string]string{"T": "v"},
	}
	step := model.PlanStep{
		Name: "s", Workflow: "wf.yaml", WorkflowDigest: digest,
		Connections: map[string]map[string]string{"ghost": {"token": "secret://a/b/c/T"}},
	}
	if _, _, err := r.runWorkflowStep(ec, job, step, false); err == nil {
		t.Fatalf("a grant for an undeclared connection must fail the step")
	}
}

func TestBuildConnectionPayloadsFailClosed(t *testing.T) {
	job := model.PlanJob{SecretRefs: []model.PlanSecretRef{{AsEnv: "T", Ref: "secret://a/b/c/T"}}}
	// A grant referencing a secret the job does not carry is a launch error.
	step := model.PlanStep{Name: "s", Connections: map[string]map[string]string{
		"conn": {"token": "secret://a/b/c/ABSENT"},
	}}
	if _, err := buildConnectionPayloads(job, step, map[string]string{"T": "v"}); err == nil {
		t.Fatalf("expected error for a grant outside the job's secretRefs")
	}
	// A granted-but-unresolved secret is a launch error too.
	step.Connections["conn"]["token"] = "secret://a/b/c/T"
	if _, err := buildConnectionPayloads(job, step, map[string]string{}); err == nil {
		t.Fatalf("expected error for an unresolved granted secret")
	}
	// No grant → nil payloads, no error.
	if got, err := buildConnectionPayloads(job, model.PlanStep{Name: "s"}, nil); err != nil || got != nil {
		t.Fatalf("no grant should yield nil: %v %v", got, err)
	}
}

func TestWorkflowRunDirProvisioned(t *testing.T) {
	dir, digest := writeWF(t)
	r := &Runner{ExecID: "exec-1"}
	ec := executor.ExecContext{Context: context.Background(), WorkspaceDir: dir}
	step := model.PlanStep{Name: "s", Workflow: "wf.yaml", WorkflowDigest: digest}

	if _, _, err := r.runWorkflowStep(ec, model.PlanJob{ID: "web@prod.deploy"}, step, false); err != nil {
		t.Fatal(err)
	}
	// The engine's file-backed run state lives under the workspace's .orun tree.
	runRoot := filepath.Join(dir, ".orun", "wfruns", "exec-1", "web@prod.deploy", "s")
	entries, err := os.ReadDir(runRoot)
	if err != nil || len(entries) == 0 {
		t.Fatalf("run state not provisioned under %s: %v", runRoot, err)
	}
	if _, err := os.Stat(filepath.Join(runRoot, entries[0].Name(), "metadata.json")); err != nil {
		t.Fatalf("run metadata missing: %v", err)
	}
}

func TestRunWorkflowStepResumeReentersFailedRun(t *testing.T) {
	// Step one counts its executions; step two fails until the release file
	// exists. A resume attempt must re-enter the SAME run and re-execute only
	// what had not succeeded (§8) — step one runs exactly once overall.
	dir := t.TempDir()
	count := filepath.Join(dir, "count.txt")
	release := filepath.Join(dir, "release")
	body := `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: resume }
inputs:
  count: { type: string, required: true }
  release: { type: string, required: true }
steps:
  - name: first
    run: ["sh", "-c", "echo x >> {{ inputs.count }}"]
  - name: second
    needs: [first]
    run: ["test", "-f", "{{ inputs.release }}"]
`
	if err := os.WriteFile(filepath.Join(dir, "wf.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	digest := flow.DigestBytes([]byte(body))
	r := &Runner{ExecID: "exec-r"}
	ec := executor.ExecContext{Context: context.Background(), WorkspaceDir: dir}
	step := model.PlanStep{
		Name: "s", Workflow: "wf.yaml", WorkflowDigest: digest, Resume: true,
		With: map[string]interface{}{"count": count, "release": release},
	}

	// First attempt: second step fails.
	if _, _, err := r.runWorkflowStep(ec, model.PlanJob{ID: "j"}, step, false); err == nil {
		t.Fatalf("first attempt should fail while the release file is absent")
	}
	// Retry attempt with resume: re-enters the same run; only `second` re-runs.
	if err := os.WriteFile(release, []byte("go"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.runWorkflowStep(ec, model.PlanJob{ID: "j"}, step, true); err != nil {
		t.Fatalf("resume attempt should succeed: %v", err)
	}
	data, err := os.ReadFile(count)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "x"); got != 1 {
		t.Fatalf("succeeded step must not re-execute on resume: ran %d times", got)
	}
}

func TestSubstituteWorkflowOutputs(t *testing.T) {
	outputs := map[string]map[string]any{
		"get-oncall": {"email": "sam@acme.dev"},
	}
	step := model.PlanStep{
		Name: "page",
		Run:  "./page.sh ${{ steps.get-oncall.outputs.email }}",
		Env:  map[string]interface{}{"ONCALL": "${{ steps.get-oncall.outputs.email }}", "N": 1},
	}
	got, err := substituteWorkflowOutputs(step, outputs)
	if err != nil {
		t.Fatalf("substitute: %v", err)
	}
	if got.Run != "./page.sh sam@acme.dev" || got.Env["ONCALL"] != "sam@acme.dev" {
		t.Fatalf("substitution incomplete: %+v", got)
	}
	// Dangling reference fails closed.
	bad := model.PlanStep{Name: "b", Run: "${{ steps.get-oncall.outputs.absent }}"}
	if _, err := substituteWorkflowOutputs(bad, outputs); err == nil {
		t.Fatalf("dangling output reference must fail the step")
	}
	// A step with no references is untouched.
	plain := model.PlanStep{Name: "p", Run: "echo hi"}
	if got, err := substituteWorkflowOutputs(plain, nil); err != nil || got.Run != "echo hi" {
		t.Fatalf("plain step should pass through: %v", err)
	}
}

func TestResolveWorkflowPath(t *testing.T) {
	r := &Runner{}
	ec := executor.ExecContext{WorkspaceDir: "/ws", WorkDir: "/ws/svc"}
	if got := r.resolveWorkflowPath(ec, "wf/notify.yaml"); got != filepath.Join("/ws", "wf/notify.yaml") {
		t.Fatalf("relative path should resolve against workspace: %q", got)
	}
	if got := r.resolveWorkflowPath(ec, "/abs/wf.yaml"); got != "/abs/wf.yaml" {
		t.Fatalf("absolute path should pass through: %q", got)
	}
}
