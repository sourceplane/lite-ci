package flow

import (
	"strings"
	"testing"
)

func TestValidateGrant(t *testing.T) {
	declared := []string{"chat-main", "vcs-app"}
	full := map[string]map[string]string{
		"chat-main": {"token": "secret://a/b/c/CHAT"},
		"vcs-app":   {"token": "secret://a/b/c/VCS"},
	}
	if err := ValidateGrant("step s", declared, full); err != nil {
		t.Fatalf("complete grant should validate: %v", err)
	}
	// Missing: error prints a paste block (S-8).
	err := ValidateGrant("step s", declared, map[string]map[string]string{"chat-main": full["chat-main"]})
	if err == nil || !strings.Contains(err.Error(), "connections:") || !strings.Contains(err.Error(), "vcs-app") {
		t.Fatalf("missing grant should print the block to paste: %v", err)
	}
	// Stale: grant names an undeclared connection.
	if err := ValidateGrant("step s", []string{"chat-main"}, full); err == nil {
		t.Fatalf("stale grant must fail")
	}
	// No declarations, no grant: fine.
	if err := ValidateGrant("step s", nil, nil); err != nil {
		t.Fatalf("empty grant over no declarations: %v", err)
	}
}

func TestConnectionAndOutputNames(t *testing.T) {
	wf, err := Parse([]byte(`apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: names }
connections:
  vcs-app: { type: http.bearer }
  chat-main: { type: http.bearer }
outputs:
  b: "'x'"
  a: "'y'"
steps:
  - name: s
    action: http.request
    connection: vcs-app
    with: { url: "https://example.invalid" }
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := wf.ConnectionNames(); len(got) != 2 || got[0] != "chat-main" || got[1] != "vcs-app" {
		t.Fatalf("ConnectionNames not sorted/complete: %v", got)
	}
	if got := wf.OutputNames(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("OutputNames not sorted/complete: %v", got)
	}
}

func TestDigestBytesMatchesDigest(t *testing.T) {
	if DigestBytes([]byte("abc")) != "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("DigestBytes format drifted: %s", DigestBytes([]byte("abc")))
	}
}
