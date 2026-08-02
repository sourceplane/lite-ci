package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/cliauth"
	"github.com/spf13/cobra"
)

// writeTestSession plants a file-store session in home. On darwin the keychain
// store fronts the file store, so session-dependent assertions are skipped
// there (same convention as command_auth_test.go).
func writeTestSession(t *testing.T, home string, orgs []cliauth.OrgRef) {
	t.Helper()
	dir := filepath.Join(home, ".orun")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.Marshal(cliauth.Credentials{
		AccessToken:       "tok",
		AccessTokenExpiry: "2099-01-01T00:00:00Z",
		User:              cliauth.SessionUser{ID: "u1", Email: "dev@example.com"},
		Orgs:              orgs,
		BackendURL:        "https://api.example.com",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "credentials.json"), data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if len(sessionOrgs()) != len(orgs) {
		t.Skip("session-dependent test needs the file credential store (keychain fronts it on this platform)")
	}
}

func testOrgs() []cliauth.OrgRef {
	return []cliauth.OrgRef{
		{ID: "org_1", WorkspaceRef: "ws_OGPIC", Slug: "ogpic", Name: "ogpic", Role: "owner"},
		{ID: "org_2", WorkspaceRef: "ws_HALO", Slug: "halo", Name: "Halo", Role: "owner"},
	}
}

// The selection is the LAST rung: it fills in for a bare command but never
// retargets a repo that links or declares its own workspace.
func TestSelectedWorkspaceIsTheLastResort(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(workspaceEnvVar, "")
	t.Setenv(orgEnvVar, "")
	if err := cliauth.SelectWorkspace(cliauth.WorkspaceSelection{ID: "ws_OGPIC", Slug: "ogpic"}); err != nil {
		t.Fatalf("SelectWorkspace: %v", err)
	}
	if s := resolveScope("", "", "", "", "", ""); s.OrgID != "ws_OGPIC" {
		t.Errorf("selection should fill in when nothing else resolves: got %q", s.OrgID)
	}
	if s := resolveScope("", "", "", "", "link-org", ""); s.OrgID != "link-org" {
		t.Errorf("repo link must outrank the selection: got %q", s.OrgID)
	}
	if s := resolveScope("", "", "intent-org", "", "", ""); s.OrgID != "intent-org" {
		t.Errorf("intent must outrank the selection: got %q", s.OrgID)
	}
	if s := resolveScope("flag-ws", "", "", "", "link-org", ""); s.OrgID != "flag-ws" {
		t.Errorf("--workspace must outrank everything: got %q", s.OrgID)
	}
	// And the source is reported honestly for each rung.
	if ws, src := workspaceSource("", "", ""); ws != "ws_OGPIC" || !strings.Contains(src, "workspace use") {
		t.Errorf("workspaceSource() = %q/%q, want the selection", ws, src)
	}
	if ws, src := workspaceSource("", "", "link-org"); ws != "link-org" || !strings.Contains(src, "link") {
		t.Errorf("workspaceSource() = %q/%q, want the repo link", ws, src)
	}
}

func TestSelectWorkspaceRoundTripAndClear(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// A selection must not clobber the rest of the config file.
	if err := cliauth.SaveConfig(&cliauth.Config{Cloud: cliauth.CloudConfig{URL: "https://api.example.com"}}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if err := cliauth.SelectWorkspace(cliauth.WorkspaceSelection{ID: "ws_OGPIC", Slug: "ogpic", SetAt: "2026-08-02T00:00:00Z"}); err != nil {
		t.Fatalf("SelectWorkspace: %v", err)
	}
	cfg, err := cliauth.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Cloud.URL != "https://api.example.com" {
		t.Errorf("selecting a workspace dropped cloud.url: %+v", cfg)
	}
	if got := cliauth.SelectedWorkspace(); got.ID != "ws_OGPIC" || got.Label() != "ogpic" {
		t.Errorf("SelectedWorkspace() = %+v", got)
	}
	if err := cliauth.SelectWorkspace(cliauth.WorkspaceSelection{}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := cliauth.SelectedWorkspace(); got.ID != "" {
		t.Errorf("clear left %+v", got)
	}
}

// `workspace use` accepts the slug, the ws_… id, or the org_… id, and persists
// the ws_… id (the primary identifier users see).
func TestWorkspaceUseAcceptsEverySpellingAndPersistsWorkspaceID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestSession(t, home, testOrgs())

	for _, spelling := range []string{"ogpic", "OGPIC", "ws_OGPIC", "org_1"} {
		if err := cliauth.SelectWorkspace(cliauth.WorkspaceSelection{}); err != nil {
			t.Fatalf("reset: %v", err)
		}
		if err := runWorkspaceUse(spelling); err != nil {
			t.Fatalf("runWorkspaceUse(%q): %v", spelling, err)
		}
		got := cliauth.SelectedWorkspace()
		if got.ID != "ws_OGPIC" || got.Slug != "ogpic" {
			t.Errorf("use %q persisted %+v, want ws_OGPIC/ogpic", spelling, got)
		}
		if strings.TrimSpace(got.SetAt) == "" {
			t.Errorf("use %q left setAt empty", spelling)
		}
	}
}

func TestWorkspaceUseUnknownSuggestsAndLists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestSession(t, home, testOrgs())

	err := runWorkspaceUse("ogpi")
	if err == nil {
		t.Fatal("expected an error for an unknown workspace")
	}
	msg := err.Error()
	if !strings.Contains(msg, "did you mean:") || !strings.Contains(msg, "orun workspace use ogpic") {
		t.Errorf("expected a did-you-mean suggestion, got:\n%s", msg)
	}
	if !strings.Contains(msg, "workspaces: ogpic, halo") {
		t.Errorf("expected the reachable workspaces to be listed, got:\n%s", msg)
	}
	// A wrong name must never be persisted.
	if got := cliauth.SelectedWorkspace(); got.ID != "" {
		t.Errorf("a failed use persisted %+v", got)
	}
}

// The error a workspace-scoped command fails with must ask for a workspace —
// NOT tell a logged-in user with workspaces that their repo isn't linked.
func TestErrWorkspaceRequiredAsksForAWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestSession(t, home, testOrgs())

	msg := errWorkspaceRequired().Error()
	for _, want := range []string{
		"specify a workspace",
		"--workspace <ws-id|slug>",
		"orun workspace use ogpic",
		"your workspaces: ogpic, halo",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "isn't connected to Orun Cloud") {
		t.Errorf("the repo-link error must not be reused for a missing workspace:\n%s", msg)
	}
}

// --workspace names WHICH workspace on every tree; --org keeps working; the
// workspace-shared RUNG is --shared.
func TestWorkspaceSelectorFlagsOnEveryTree(t *testing.T) {
	root := &cobra.Command{Use: "orun"}
	registerSecretsCommand(root)
	registerIntegrationsCommand(root)
	registerPolicyCommand(root)

	for _, name := range []string{"secrets", "integrations", "policy"} {
		cmd := findCommand(t, root, name)
		ws := cmd.PersistentFlags().Lookup("workspace")
		if ws == nil {
			t.Fatalf("%s: --workspace is not registered", name)
		}
		if ws.Value.Type() != "string" {
			t.Errorf("%s: --workspace must take a workspace, got type %q", name, ws.Value.Type())
		}
		if org := cmd.PersistentFlags().Lookup("org"); org == nil {
			t.Errorf("%s: the legacy --org spelling must keep working", name)
		}
	}

	set := findCommand(t, findCommand(t, root, "secrets"), "set")
	if set.Flags().Lookup("shared") == nil {
		t.Error("secrets set: the workspace-shared rung must be --shared")
	}
	if f := set.Flags().Lookup("workspace"); f != nil && f.Value.Type() == "bool" {
		t.Error("secrets set: --workspace must not be the rung boolean any more")
	}
}

func findCommand(t *testing.T, parent *cobra.Command, name string) *cobra.Command {
	t.Helper()
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	t.Fatalf("command %q was not registered under %q", name, parent.Name())
	return nil
}

// The two halves of the --workspace migration: pflag's bare "needs an argument"
// gets the new spelling appended, and `--workspace --json` is refused instead of
// sending "--json" to the backend as a tenancy.
func TestWorkspaceFlagMigrationErrors(t *testing.T) {
	err := decorateWorkspaceFlagError(nil, errNeedsArgument())
	if err == nil || !strings.Contains(err.Error(), "--shared") {
		t.Errorf("expected the --shared migration hint, got: %v", err)
	}
	unrelated := errUnrelatedFlag()
	if got := decorateWorkspaceFlagError(nil, unrelated); got != unrelated {
		t.Errorf("unrelated flag errors must pass through unchanged, got: %v", got)
	}

	prev := secretsOrgFlag
	t.Cleanup(func() { secretsOrgFlag = prev })
	secretsOrgFlag = "--json"
	err = validateWorkspaceFlagValue()
	if err == nil || !strings.Contains(err.Error(), "that is a flag, not a workspace") {
		t.Errorf("expected a flag-as-value rejection, got: %v", err)
	}
	secretsOrgFlag = "ws_OGPIC"
	if err := validateWorkspaceFlagValue(); err != nil {
		t.Errorf("a real workspace must pass: %v", err)
	}
}

func errNeedsArgument() error { return &simpleErr{"flag needs an argument: --workspace"} }
func errUnrelatedFlag() error { return &simpleErr{"unknown flag: --nope"} }

type simpleErr struct{ msg string }

func (e *simpleErr) Error() string { return e.msg }
