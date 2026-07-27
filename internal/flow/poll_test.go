package flow

import (
	"os"
	"path/filepath"
	"time"
	"strings"
	"testing"
)

// A run: step that fails until a marker file exists — the shape of every
// wait-for-CI loop: non-zero exits are evaluable attempts, not faults.
func TestPollUntilSatisfied(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ready")
	go func() {
		// Created ~0.5s in: a couple of poll attempts must happen first.
		<-timeCh(500)
		_ = os.WriteFile(marker, []byte("ok"), 0o644)
	}()
	res := run(t, `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: p }
inputs:
  marker: { type: string, required: true }
steps:
  - name: wait
    run: ["cat", "{{ inputs.marker }}"]
    poll: { interval: 200ms, timeout: 10s, until: "exitCode == 0" }
    outputs: { content: "stdout.trim()" }
`, RunOptions{Inputs: map[string]any{"marker": marker}})
	if res.Status != "succeeded" {
		t.Fatalf("poll: %+v", res.Steps["wait"])
	}
	st := res.Steps["wait"]
	if st.Attempts < 2 {
		t.Fatalf("expected multiple attempts, got %d", st.Attempts)
	}
	if st.Outputs["content"] != "ok" {
		t.Fatalf("outputs sealed from the satisfying attempt: %#v", st.Outputs)
	}
}

func TestPollTimeoutFails(t *testing.T) {
	res := run(t, `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: p }
steps:
  - name: wait
    run: ["false"]
    poll: { interval: 100ms, timeout: 500ms, until: "exitCode == 0" }
`, RunOptions{})
	st := res.Steps["wait"]
	if res.Status != "failed" || !strings.Contains(st.Error, "not satisfied within") {
		t.Fatalf("poll timeout: %+v", st)
	}
	if st.Attempts < 2 {
		t.Fatalf("expected repeated attempts before timeout, got %d", st.Attempts)
	}
}

func TestPollPlusRetryRejected(t *testing.T) {
	mustFail(t, `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: p }
steps:
  - name: a
    run: ["true"]
    retry: { maxRetries: 2 }
    poll: { interval: 1s, timeout: 5s, until: "exitCode == 0" }
`, "mutually exclusive")
}

func timeCh(ms int) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		time.Sleep(time.Duration(ms) * time.Millisecond)
		close(ch)
	}()
	return ch
}
