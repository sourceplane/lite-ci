package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/worklens"
)

func runWorkCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := &cobra.Command{Use: "orun", SilenceUsage: true, SilenceErrors: true}
	registerWorkCommand(root)
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func TestWorkImportDryRunJSON(t *testing.T) {
	out, err := runWorkCmd(t, "work", "import", "../../internal/worklens/testdata/spectree", "--workspace", "ws_test", "--dry-run", "--json")
	if err != nil {
		t.Fatalf("import --dry-run failed: %v\n%s", err, out)
	}
	var plan worklens.ImportPlan
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatalf("output is not a plan: %v\n%s", err, out)
	}
	if plan.Workspace != "ws_test" || len(plan.Specs) != 2 || len(plan.Tasks) != 2 {
		t.Fatalf("plan = %d specs, %d tasks, ws %q", len(plan.Specs), len(plan.Tasks), plan.Workspace)
	}
}

func TestWorkImportApplyNeedsBackend(t *testing.T) {
	// Apply (no --dry-run) must resolve a backend + workspace before any
	// write; in a bare test environment that resolution fails loudly rather
	// than silently doing nothing.
	out, err := runWorkCmd(t, "work", "import", "../../internal/worklens/testdata/spectree", "--workspace", "ws_test")
	if err == nil {
		t.Fatalf("apply succeeded without a backend:\n%s", out)
	}
}

func TestWorkImportHuman(t *testing.T) {
	out, err := runWorkCmd(t, "work", "import", "../../internal/worklens/testdata/spectree", "--workspace", "ws_test", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"initiatives: 2", "specs:       2", "milestones:  2", "tasks:       2", "demo-epic", "dry run"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// WK4 (orun-work-spaces): `orun work` leads; `orun initiatives` is the
// hidden deprecated alias serving the SAME roster — the exact inverse of
// IN-F, and shared constructors mean the alias can never drift (WK-6).
func TestWorkGroupLeadsAndInitiativesAliases(t *testing.T) {
	root := &cobra.Command{Use: "orun"}
	registerWorkCommand(root)
	registerInitiativesAliasCommand(root)
	var work, initiatives *cobra.Command
	for _, c := range root.Commands() {
		switch c.Name() {
		case "work":
			work = c
		case "initiatives":
			initiatives = c
		}
	}
	if work == nil || work.Hidden {
		t.Fatal("orun work must lead (registered and visible)")
	}
	if initiatives == nil || !initiatives.Hidden || initiatives.Deprecated == "" {
		t.Fatal("orun initiatives must be the hidden deprecated alias")
	}
	names := func(c *cobra.Command) map[string]bool {
		m := map[string]bool{}
		for _, s := range c.Commands() {
			m[s.Name()] = true
		}
		return m
	}
	w, i := names(work), names(initiatives)
	for n := range w {
		if !i[n] {
			t.Errorf("alias is missing %q — it must forward the whole roster", n)
		}
	}
	for _, n := range []string{"spaces", "epics", "start", "update", "adopt", "yours"} {
		if !w[n] {
			t.Errorf("work group is missing %q", n)
		}
	}
}
