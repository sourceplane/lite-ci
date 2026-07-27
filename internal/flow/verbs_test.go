package flow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const runWF = `apiVersion: orun.dev/v1
kind: Workflow
metadata:
  name: rv
inputs:
  msg: { type: string, default: "a;b && c" }
outputs:
  said: "steps.say.outputs.text"
steps:
  - name: say
    run: ["echo", "{{ inputs.msg }}"]
    env: { DECLARED: "yes" }
    outputs:
      text: "stdout.trim()"
      code: "exitCode"
  - name: leak-check
    needs: [say]
    run: ["/bin/sh", "-c", "echo [$DECLARED][$LEAKME]"]
    env: { DECLARED: "d" }
    outputs:
      text: "stdout.trim()"
`

func TestRunVerbLiteralArgvAndEnvAllowlist(t *testing.T) {
	t.Setenv("LEAKME", "secret")
	res := run(t, runWF, RunOptions{})
	if res.Status != "succeeded" {
		t.Fatalf("status: %+v steps: say=%+v", res, res.Steps["say"])
	}
	// Shell metachars are literal arguments, not interpreted.
	if res.Outputs["said"] != "a;b && c" {
		t.Fatalf("argv literality: %#v", res.Outputs)
	}
	if res.Steps["say"].Outputs["code"] != int64(0) {
		t.Fatalf("exitCode output: %#v", res.Steps["say"].Outputs)
	}
	// Declared env passes; ambient env does not survive the allowlist.
	if got := res.Steps["leak-check"].Outputs["text"]; got != "[d][]" {
		t.Fatalf("env allowlist: %#v", got)
	}
}

func TestRunVerbNonZeroExitFails(t *testing.T) {
	res := run(t, `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: x }
steps:
  - name: bad
    run: ["/bin/sh", "-c", "echo oops >&2; exit 3"]
`, RunOptions{})
	if res.Status != "failed" || res.Steps["bad"].ExitCode != 3 || !strings.Contains(res.Steps["bad"].Error, "exited 3") {
		t.Fatalf("non-zero exit: %+v", res.Steps["bad"])
	}
	if !strings.Contains(res.Steps["bad"].Stderr, "oops") {
		t.Fatalf("stderr capture: %q", res.Steps["bad"].Stderr)
	}
}

func TestRunVerbTimeoutKills(t *testing.T) {
	start := time.Now()
	res := run(t, `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: x }
steps:
  - name: slow
    run: ["sleep", "10"]
    timeout: 300ms
`, RunOptions{})
	if res.Status != "failed" || !strings.Contains(res.Steps["slow"].Error, "timed out") {
		t.Fatalf("timeout: %+v", res.Steps["slow"])
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("kill was not prompt: %v", time.Since(start))
	}
}

func TestRunVerbDefaultCwdIsRunDir(t *testing.T) {
	root := t.TempDir()
	wf := mustParse(t, `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: x }
steps:
  - name: where
    run: ["pwd"]
    outputs: { dir: "stdout.trim()" }
`)
	res, err := Run(context.Background(), wf, RunOptions{RunRoot: root})
	if err != nil || res.Status != "succeeded" {
		t.Fatalf("run: %v %+v", err, res)
	}
	got := res.Steps["where"].Outputs["dir"].(string)
	want := filepath.Join(root, res.ExecID)
	// Resolve symlinks (macOS /var → /private/var) before comparing.
	rg, _ := filepath.EvalSymlinks(got)
	rw, _ := filepath.EvalSymlinks(want)
	if rg != rw {
		t.Fatalf("cwd: got %q want %q", got, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatal(err)
	}
}

func TestRunVerbEnvInherit(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/tmp/agent.sock")
	t.Setenv("NOT_DECLARED", "x")
	res := run(t, `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: x }
steps:
  - name: a
    run: ["/bin/sh", "-c", "echo [$SSH_AUTH_SOCK][$NOT_DECLARED]"]
    envInherit: [SSH_AUTH_SOCK]
    outputs: { t: "stdout.trim()" }
`, RunOptions{})
	if res.Steps["a"].Outputs["t"] != "[/tmp/agent.sock][]" {
		t.Fatalf("envInherit: %#v", res.Steps["a"].Outputs)
	}
}
