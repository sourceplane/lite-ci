package flow

import (
	"strings"
	"testing"
)

const good = `apiVersion: orun.dev/v1
kind: Workflow
metadata:
  name: baseline-flow
inputs:
  repoName: { type: string, required: true, pattern: "^[a-z][a-z0-9-]*$" }
connections:
  gh: { type: http.bearer }
outputs:
  prUrl: "steps.open-pr.outputs.url"
steps:
  - name: scaffold
    run: ["echo", "{{ inputs.repoName }}"]
    env: { ORUN_VERBOSE: "1" }
    outputs:
      dir: "stdout"
  - name: open-pr
    needs: [scaffold]
    run: ["echo", "https://example.com/pr/1"]
    outputs:
      url: "stdout"
  - name: wait-ci
    needs: [open-pr]
    action: http.request
    connection: gh
    with: { url: "{{ steps.open-pr.outputs.url }}", method: GET }
    poll: { interval: 1s, timeout: 5s, until: "output.status == 200" }
  - name: merge
    needs: [wait-ci]
    if: "steps.wait-ci.outputs != null"
    run: ["echo", "merged"]
`

func mustParse(t *testing.T, src string) *Workflow {
	t.Helper()
	wf, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return wf
}

func mustFail(t *testing.T, src, wantSub string) {
	t.Helper()
	_, err := Parse([]byte(src))
	if err == nil || !strings.Contains(err.Error(), wantSub) {
		t.Fatalf("want error containing %q, got %v", wantSub, err)
	}
}

func TestGoodWorkflowValidates(t *testing.T) {
	wf := mustParse(t, good)
	if len(wf.Steps) != 4 || wf.Steps[2].Verb() != "action" {
		t.Fatalf("unexpected parse result")
	}
}

func TestTorkflowRejected(t *testing.T) {
	mustFail(t, "apiVersion: torkflow/v1\nkind: Workflow\n", "torkflow/v1 workflows are not supported")
}

func TestUnknownActionFailsValidate(t *testing.T) {
	mustFail(t, `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: x }
steps:
  - name: a
    action: slack.post
`, "unknown action")
}

func TestUncompilableExpressionFails(t *testing.T) {
	mustFail(t, `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: x }
steps:
  - name: a
    run: ["true"]
    if: "steps..broken ==="
`, "if")
}

func TestUndeclaredReferencesFail(t *testing.T) {
	mustFail(t, `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: x }
steps:
  - name: a
    run: ["echo", "{{ inputs.nope }}"]
`, "undeclared inputs.nope")
	mustFail(t, `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: x }
steps:
  - name: a
    run: ["true"]
  - name: b
    run: ["true"]
    if: "steps.a.outputs != null"
`, "not a declared upstream step") // a is not in b's needs — parallel read rejected
}

func TestCycleAndVerbRules(t *testing.T) {
	mustFail(t, `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: x }
steps:
  - name: a
    needs: [b]
    run: ["true"]
  - name: b
    needs: [a]
    run: ["true"]
`, "cycle")
	mustFail(t, `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: x }
steps:
  - name: a
    run: ["true"]
    action: sleep
`, "exactly one of run/action/workflow")
	mustFail(t, `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: x }
steps:
  - name: a
    action: sleep
    poll: { interval: 1s, timeout: 5s }
`, "poll.until is required")
	mustFail(t, `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: x }
steps:
  - name: a
    action: sleep
    connection: nope
`, "not declared")
}

func TestDashedStepRewrite(t *testing.T) {
	v, err := EvalExpr("steps.open-pr.outputs.url", map[string]any{
		"steps": map[string]any{"open-pr": map[string]any{"outputs": map[string]any{"url": "u"}}},
	})
	if err != nil || v != "u" {
		t.Fatalf("dash rewrite eval: %v %v", v, err)
	}
}

func TestInterpolate(t *testing.T) {
	vars := map[string]any{"inputs": map[string]any{"n": "acme"}, "exitCode": 3}
	v, err := Interpolate("repo-{{ inputs.n }}", vars)
	if err != nil || v != "repo-acme" {
		t.Fatalf("interp: %v %v", v, err)
	}
	v, err = Interpolate("{{ exitCode }}", vars)
	if err != nil || v != int64(3) {
		t.Fatalf("typed interp: %#v %v", v, err)
	}
}

func TestActionValidation(t *testing.T) {
	if _, err := ResolveAction("http.request"); err != nil {
		t.Fatal(err)
	}
	a, _ := ResolveAction("sleep")
	if err := a.Validate(map[string]any{"duration": "50ms"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Validate(map[string]any{}); err == nil {
		t.Fatal("sleep without duration must fail validation")
	}
}
