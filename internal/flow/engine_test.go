package flow

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const engineWF = `apiVersion: orun.dev/v1
kind: Workflow
metadata:
  name: eng
inputs:
  who: { type: string, default: "world" }
outputs:
  greeting: "steps.compose.outputs.text"
steps:
  - name: compose
    action: script
    with:
      source: "'hello ' + input"
      input: "{{ inputs.who }}"
    outputs:
      text: "output.value"
  - name: gate
    needs: [compose]
    if: "steps.compose.outputs.text == 'hello world'"
    action: sleep
    with: { duration: "1ms" }
  - name: after
    needs: [gate]
    action: script
    with: { source: "'done'" }
    outputs:
      v: "output.value"
`

func run(t *testing.T, src string, opts RunOptions) *RunResult {
	t.Helper()
	wf := mustParse(t, src)
	if opts.RunRoot == "" {
		opts.RunRoot = t.TempDir()
	}
	res, err := Run(context.Background(), wf, opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return res
}

func TestEngineChainOutputsAndSkip(t *testing.T) {
	res := run(t, engineWF, RunOptions{})
	if res.Status != "succeeded" {
		t.Fatalf("status: %+v", res)
	}
	if res.Outputs["greeting"] != "hello world" {
		t.Fatalf("workflow outputs: %#v", res.Outputs)
	}
	if res.Steps["gate"].Status != "succeeded" || res.Steps["after"].Outputs["v"] != "done" {
		t.Fatalf("steps: %#v", res.Steps)
	}

	// Same workflow, input that fails the gate: skipped, dependent still runs.
	res = run(t, engineWF, RunOptions{Inputs: map[string]any{"who": "moon"}})
	if res.Steps["gate"].Status != "skipped" || res.Steps["after"].Status != "succeeded" {
		t.Fatalf("skip semantics: gate=%s after=%s", res.Steps["gate"].Status, res.Steps["after"].Status)
	}
}

const failWF = `apiVersion: orun.dev/v1
kind: Workflow
metadata:
  name: f
steps:
  - name: boom
    action: script
    with: { source: "unknownFn()" }
    retry: { maxRetries: 2, baseDelay: 1ms }
  - name: never
    needs: [boom]
    action: sleep
    with: { duration: "1ms" }
  - name: tolerated
    action: script
    with: { source: "alsoBroken()" }
    continueOnError: true
`

func TestFailureBlocksAndContinueOnError(t *testing.T) {
	res := run(t, failWF, RunOptions{})
	if res.Status != "failed" {
		t.Fatalf("want failed, got %s", res.Status)
	}
	if res.Steps["boom"].Attempts != 3 {
		t.Fatalf("retry attempts: %d", res.Steps["boom"].Attempts)
	}
	if res.Steps["never"].Status != "blocked" {
		t.Fatalf("dependent of failed step: %s", res.Steps["never"].Status)
	}
	if res.Steps["tolerated"].Status != "failed" {
		t.Fatalf("continueOnError step records its failure: %s", res.Steps["tolerated"].Status)
	}
}

const resumeWF = `apiVersion: orun.dev/v1
kind: Workflow
metadata:
  name: r
steps:
  - name: a
    action: script
    with: { source: "'fresh'" }
    outputs: { v: "output.value" }
  - name: b
    needs: [a]
    action: script
    with:
      source: "'b:' + input"
      input: "{{ steps.a.outputs.v }}"
    outputs: { v: "output.value" }
`

func TestResumeSkipsSucceededSteps(t *testing.T) {
	root := t.TempDir()
	execID := "r-test"
	// Seed run state: step a already succeeded with a sentinel output; b failed.
	mustMkdir(t, filepath.Join(root, execID, "steps"))
	if err := writeJSON(filepath.Join(root, execID, "metadata.json"), map[string]any{"digest": "sha256:x"}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(root, execID, "steps", "a.json"), &StepState{Name: "a", Status: "succeeded", Outputs: map[string]any{"v": "seeded"}}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(root, execID, "steps", "b.json"), &StepState{Name: "b", Status: "failed", Error: "induced"}); err != nil {
		t.Fatal(err)
	}

	res := run(t, resumeWF, RunOptions{RunRoot: root, ExecID: execID, Digest: "sha256:x"})
	if res.Status != "succeeded" {
		t.Fatalf("resume status: %+v", res)
	}
	// a was NOT re-executed (kept its seeded output); b re-ran and read it.
	if res.Steps["a"].Outputs["v"] != "seeded" || res.Steps["b"].Outputs["v"] != "b:seeded" {
		t.Fatalf("resume outputs: a=%v b=%v", res.Steps["a"].Outputs, res.Steps["b"].Outputs)
	}
}

func TestResumeRefusesChangedDigest(t *testing.T) {
	root := t.TempDir()
	execID := "r2"
	mustMkdir(t, filepath.Join(root, execID, "steps"))
	if err := writeJSON(filepath.Join(root, execID, "metadata.json"), map[string]any{"digest": "sha256:old"}); err != nil {
		t.Fatal(err)
	}
	wf := mustParse(t, resumeWF)
	_, err := Run(context.Background(), wf, RunOptions{RunRoot: root, ExecID: execID, Digest: "sha256:new"})
	if err == nil || !strings.Contains(err.Error(), "digest changed") {
		t.Fatalf("want digest-changed refusal, got %v", err)
	}
}

func TestUnknownInputRejected(t *testing.T) {
	wf := mustParse(t, resumeWF)
	_, err := Run(context.Background(), wf, RunOptions{RunRoot: t.TempDir(), Inputs: map[string]any{"nope": 1}})
	if err == nil || !strings.Contains(err.Error(), "unknown input") {
		t.Fatalf("want unknown-input error, got %v", err)
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestRun_StreamsStepOutputLive(t *testing.T) {
	// Step output must reach the engine log line-by-line, prefixed with the
	// step name — before this, a long-polling step looked like a hang and a
	// failed step never showed its stderr on the terminal.
	wf := &Workflow{
		Metadata: Metadata{Name: "stream-test"},
		Steps: []Step{
			{Name: "talker", Run: []string{"/bin/sh", "-c", "echo hello-out; echo hello-err >&2"}},
		},
	}
	var log bytes.Buffer
	res, err := Run(context.Background(), wf, RunOptions{Dir: t.TempDir(), Log: &log})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if res.Status != "succeeded" {
		t.Fatalf("status = %s, want succeeded", res.Status)
	}
	out := log.String()
	for _, want := range []string{"▸ talker", "talker │ hello-out", "talker │ hello-err"} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q; got:\n%s", want, out)
		}
	}
}

func TestRun_StepsResolveSelfOrun(t *testing.T) {
	// A step invoking `orun` must resolve THE binary running the workflow
	// (via the run-dir shim), not whatever stale install shadows it on PATH.
	wf := &Workflow{
		Metadata: Metadata{Name: "shim-test"},
		Steps: []Step{
			{Name: "which", Run: []string{"/bin/sh", "-c", "command -v orun"}},
		},
	}
	var log bytes.Buffer
	res, err := Run(context.Background(), wf, RunOptions{Dir: t.TempDir(), Log: &log})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	st := res.Steps["which"]
	if st == nil || st.Status != "succeeded" {
		t.Fatalf("step state: %+v", st)
	}
	if !strings.Contains(st.Stdout, ".orun-bin/orun") {
		t.Errorf("step resolved orun outside the shim: %q", st.Stdout)
	}
}
