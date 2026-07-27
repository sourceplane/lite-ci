package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/flow"
)

func newCapCmd() (*cobra.Command, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	c := &cobra.Command{}
	c.SetOut(buf)
	c.SetErr(buf)
	return c, buf
}

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const goodFlowYAML = `apiVersion: orun.dev/v1
kind: Workflow
metadata:
  name: hello
steps:
  - name: say
    run: ["echo", "hi"]
`

func TestWorkflowValidate(t *testing.T) {
	dir := t.TempDir()
	good := writeFile(t, dir, "wf.yaml", goodFlowYAML)
	old := writeFile(t, dir, "old.yaml", "apiVersion: torkflow/v1\nkind: Workflow\n")
	bad := writeFile(t, dir, "no.yaml", "apiVersion: sourceplane.io/v1\nkind: Component\n")

	c, buf := newCapCmd()
	if err := runWorkflowValidate(c, good); err != nil {
		t.Fatalf("validate good: %v", err)
	}
	if !strings.Contains(buf.String(), "ok:") {
		t.Fatalf("expected ok output, got %q", buf.String())
	}
	// torkflow/v1 is rejected outright — no converter, no compat window (v3 §12).
	if err := runWorkflowValidate(c, old); err == nil || !strings.Contains(err.Error(), "torkflow/v1 workflows are not supported") {
		t.Fatalf("expected torkflow/v1 rejection, got %v", err)
	}
	if err := runWorkflowValidate(c, bad); err == nil {
		t.Fatalf("expected validation error for a non-workflow file")
	}
}

func TestWorkflowDigestCmd(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "wf.yaml", goodFlowYAML)

	c, buf := newCapCmd()
	workflowDigestCmd.SetOut(buf)
	if err := workflowDigestCmd.RunE(c, []string{p}); err != nil {
		t.Fatalf("digest: %v", err)
	}
	if strings.TrimSpace(buf.String()) != flow.DigestBytes([]byte(goodFlowYAML)) {
		t.Fatalf("digest output mismatch: %q", buf.String())
	}
}

func TestWorkflowRunInProcess(t *testing.T) {
	dir := t.TempDir()
	wf := writeFile(t, dir, "wf.yaml", `apiVersion: orun.dev/v1
kind: Workflow
metadata:
  name: hello
outputs:
  v: "steps.say.outputs.v"
steps:
  - name: say
    action: script
    with: { source: "'hi'" }
    outputs: { v: "output.value" }
`)
	t.Chdir(dir) // run state lands under the cwd's .orun/wfruns
	workflowRunSet, workflowRunConnections, workflowRunResume = nil, nil, ""

	c, buf := newCapCmd()
	if err := runWorkflowRun(context.Background(), c, wf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "succeeded") || !strings.Contains(buf.String(), "v = hi") {
		t.Fatalf("expected success summary with outputs, got %q", buf.String())
	}
}

func TestWorkflowRunFailureIsError(t *testing.T) {
	dir := t.TempDir()
	wf := writeFile(t, dir, "wf.yaml", `apiVersion: orun.dev/v1
kind: Workflow
metadata:
  name: boom
steps:
  - name: a
    action: script
    with: { source: "nope()" }
`)
	t.Chdir(dir)
	workflowRunSet, workflowRunConnections, workflowRunResume = nil, nil, ""

	c, _ := newCapCmd()
	if err := runWorkflowRun(context.Background(), c, wf); err == nil {
		t.Fatalf("expected error for a failed workflow run")
	}
}

func TestWorkflowViewInProcess(t *testing.T) {
	dir := t.TempDir()
	wf := writeFile(t, dir, "wf.yaml", goodFlowYAML)
	c, buf := newCapCmd()
	if err := runWorkflowView(context.Background(), c, wf); err != nil {
		t.Fatalf("view: %v", err)
	}
	if !strings.Contains(buf.String(), "say [run]") {
		t.Fatalf("view output: %q", buf.String())
	}
}

func TestParseSetFlags(t *testing.T) {
	got, err := parseSetFlags([]string{"a=1", "b=two"})
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != "1" || got["b"] != "two" {
		t.Fatalf("unexpected: %#v", got)
	}
	if _, err := parseSetFlags([]string{"bad"}); err == nil {
		t.Fatalf("expected error for a flag without =")
	}
}
