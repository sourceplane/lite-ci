package scaffold

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/flow"
	"github.com/sourceplane/orun/internal/objectstore"
)

const hookBP = `apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: svc
modules:
  - name: worker
    mode: template
    files:
      "README.md": "# hi\n"
hooks:
  postInstantiate:
    - id: open-pr
      workflow: hook.yaml
      with: { branch: scaffold/x }
`

// hookWF records the branch input it ran with into its run dir — the
// observable stand-in for "the engine was invoked with the declared inputs"
// now that the engine is in-process.
const hookWF = `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: open-pr }
inputs:
  branch: { type: string, required: true }
steps:
  - name: record
    run: ["sh", "-c", "printf %s \"$BRANCH\" > branch.txt"]
    env: { BRANCH: "{{ inputs.branch }}" }
`

func writeHookWorkflow(t *testing.T) (baseDir, digest string) {
	t.Helper()
	baseDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(baseDir, "hook.yaml"), []byte(hookWF), 0o644); err != nil {
		t.Fatal(err)
	}
	return baseDir, flow.DigestBytes([]byte(hookWF))
}

// hookRunRecord scans the scaffold output's workflow run state for a file the
// hook workflow wrote, returning its content ("" when the hook never ran).
func hookRunRecord(t *testing.T, outDir, name string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(outDir, ".orun", "wfruns", "*", name))
	if err != nil || len(matches) == 0 {
		return ""
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestHookMutualExclusionValidation(t *testing.T) {
	both := `apiVersion: orun.dev/v1
kind: Blueprint
metadata: { name: svc }
modules:
  - { name: m, mode: template, files: { "R.md": "x" } }
hooks:
  postInstantiate:
    - id: bad
      run: ["echo", "hi"]
      workflow: hook.yaml
`
	if _, err := ParseBlueprint([]byte(both)); err == nil {
		t.Fatalf("expected validation error for a hook with both run and workflow")
	}

	neither := `apiVersion: orun.dev/v1
kind: Blueprint
metadata: { name: svc }
modules:
  - { name: m, mode: template, files: { "R.md": "x" } }
hooks:
  postInstantiate:
    - { id: empty }
`
	if _, err := ParseBlueprint([]byte(neither)); err == nil {
		t.Fatalf("expected validation error for a hook with neither run nor workflow")
	}
}

func TestWorkflowHookRunsAndIsPinned(t *testing.T) {
	baseDir, digest := writeHookWorkflow(t)
	outDir := t.TempDir()

	res, err := Run(context.Background(), Options{
		Blueprint:     []byte(hookBP),
		OutDir:        outDir,
		Store:         objectstore.NewMemStore(objectstore.AlgoSHA256),
		SourceBaseDir: baseDir,
		RunHooks:      true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The hook ran through the in-process engine with its declared inputs.
	if got := hookRunRecord(t, outDir, "branch.txt"); got != "scaffold/x" {
		t.Fatalf("workflow hook not invoked with inputs: recorded %q", got)
	}
	if len(res.HooksRun) != 1 || res.HooksRun[0] != "open-pr" {
		t.Fatalf("expected open-pr to run, got %v", res.HooksRun)
	}
	// Provenance pins the hook by reference + digest — never an outcome.
	if len(res.Provenance.Hooks) != 1 {
		t.Fatalf("expected 1 pinned hook, got %d", len(res.Provenance.Hooks))
	}
	ph := res.Provenance.Hooks[0]
	if ph.ID != "open-pr" || ph.Workflow != "hook.yaml" || ph.Digest != digest {
		t.Fatalf("hook not pinned faithfully: %#v (want digest %s)", ph, digest)
	}
}

func TestWorkflowHookPinnedEvenWithoutRunHooks(t *testing.T) {
	baseDir, digest := writeHookWorkflow(t)
	outDir := t.TempDir()
	res, err := Run(context.Background(), Options{
		Blueprint:     []byte(hookBP),
		OutDir:        outDir,
		Store:         objectstore.NewMemStore(objectstore.AlgoSHA256),
		SourceBaseDir: baseDir,
		RunHooks:      false, // hooks not executed …
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.HooksRun) != 0 {
		t.Fatalf("hooks should not run when RunHooks is off: %v", res.HooksRun)
	}
	if got := hookRunRecord(t, outDir, "branch.txt"); got != "" {
		t.Fatalf("workflow must not execute when RunHooks is off: recorded %q", got)
	}
	// … but the lineage is still pinned.
	if len(res.Provenance.Hooks) != 1 || res.Provenance.Hooks[0].Digest != digest {
		t.Fatalf("hook digest must be pinned even without --run-hooks: %#v", res.Provenance.Hooks)
	}
}

func TestWorkflowHookFailureLeavesTreeInPlace(t *testing.T) {
	baseDir := t.TempDir()
	failWF := `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: open-pr }
inputs:
  branch: { type: string, required: true }
steps:
  - name: explode
    run: ["false"]
`
	if err := os.WriteFile(filepath.Join(baseDir, "hook.yaml"), []byte(failWF), 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()

	_, err := Run(context.Background(), Options{
		Blueprint:     []byte(hookBP),
		OutDir:        out,
		Store:         objectstore.NewMemStore(objectstore.AlgoSHA256),
		SourceBaseDir: baseDir,
		RunHooks:      true,
	})
	if err == nil || !strings.Contains(err.Error(), "hook") {
		t.Fatalf("expected a hook failure error, got %v", err)
	}
	// The gated tree was written before the hook ran — it stays in place.
	if _, statErr := os.Stat(filepath.Join(out, "README.md")); statErr != nil {
		t.Fatalf("scaffold tree should be materialized despite the hook failure: %v", statErr)
	}
}

const connHookWF = `apiVersion: orun.dev/v1
kind: Workflow
metadata: { name: open-pr }
inputs:
  url: { type: string, required: true }
connections:
  vcs-app: { type: http.bearer }
steps:
  - name: open
    action: http.request
    connection: vcs-app
    with: { url: "{{ inputs.url }}" }
`

func grantHookBP(url string) string {
	return fmt.Sprintf(`apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: svc
inputs:
  vcsToken: { type: string, required: true, secret: true }
  plainInput: { type: string, required: true }
modules:
  - name: worker
    mode: template
    files:
      "README.md": "# hi\n"
hooks:
  postInstantiate:
    - id: open-pr
      workflow: hook.yaml
      with: { url: "%s" }
      connections:
        vcs-app:
          token: vcsToken
`, url)
}

func writeConnHookWF(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hook.yaml"), []byte(connHookWF), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestHookGrantInjectsMappedOnly(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotAuth = req.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	baseDir := writeConnHookWF(t)
	// The http.bearer connection injects the granted field as the bearer token.
	bp := strings.Replace(grantHookBP(srv.URL), "token: vcsToken", "bearerToken: vcsToken", 1)

	_, err := Run(context.Background(), Options{
		Blueprint: []byte(bp),
		Inputs: map[string]string{
			"vcsToken":   "ghp_hook_secret",
			"plainInput": "not-a-secret",
		},
		OutDir:        t.TempDir(),
		Store:         objectstore.NewMemStore(objectstore.AlgoSHA256),
		SourceBaseDir: baseDir,
		RunHooks:      true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotAuth != "Bearer ghp_hook_secret" {
		t.Fatalf("granted input not injected under its connection name: %q", gotAuth)
	}
	// Only the granted connection crosses, holding exactly the mapped fields.
	hr := &hookRunner{secrets: map[string]any{"vcsToken": "ghp_hook_secret", "plainInput": "not-a-secret"}}
	conns, cerr := hr.connectionPayloads(Hook{ID: "open-pr", Connections: map[string]map[string]string{"vcs-app": {"bearerToken": "vcsToken"}}})
	if cerr != nil {
		t.Fatal(cerr)
	}
	if len(conns) != 1 || len(conns["vcs-app"]) != 1 || conns["vcs-app"]["bearerToken"] != "ghp_hook_secret" {
		t.Fatalf("only the granted connection may cross: %#v", conns)
	}
}

func TestHookMissingGrantFailsBeforeAnyWrite(t *testing.T) {
	baseDir := writeConnHookWF(t)
	out := t.TempDir()
	noGrant := strings.Replace(grantHookBP("https://vcs.invalid"), "      connections:\n        vcs-app:\n          token: vcsToken\n", "", 1)

	_, err := Run(context.Background(), Options{
		Blueprint:     []byte(noGrant),
		Inputs:        map[string]string{"vcsToken": "x", "plainInput": "y"},
		OutDir:        out,
		Store:         objectstore.NewMemStore(objectstore.AlgoSHA256),
		SourceBaseDir: baseDir,
	})
	if err == nil || !strings.Contains(err.Error(), "connections:") {
		t.Fatalf("expected a grant error with the paste block, got %v", err)
	}
	// Fail-closed BEFORE placement: nothing may be written.
	if _, statErr := os.Stat(filepath.Join(out, "README.md")); !os.IsNotExist(statErr) {
		t.Fatalf("grant failure must abort before any file is written")
	}
}

func TestHookGrantRequiresSecretInput(t *testing.T) {
	baseDir := writeConnHookWF(t)
	nonSecret := strings.Replace(grantHookBP("https://vcs.invalid"), "token: vcsToken", "token: plainInput", 1)
	_, err := Run(context.Background(), Options{
		Blueprint:     []byte(nonSecret),
		Inputs:        map[string]string{"vcsToken": "x", "plainInput": "y"},
		OutDir:        t.TempDir(),
		Store:         objectstore.NewMemStore(objectstore.AlgoSHA256),
		SourceBaseDir: baseDir,
	})
	if err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("a grant drawing from a non-secret input must fail: %v", err)
	}
}

func TestWorkflowHookMissingFileIsError(t *testing.T) {
	// No hook.yaml on disk: the reference cannot be pinned → fail-closed.
	_, err := Run(context.Background(), Options{
		Blueprint:     []byte(hookBP),
		OutDir:        t.TempDir(),
		Store:         objectstore.NewMemStore(objectstore.AlgoSHA256),
		SourceBaseDir: t.TempDir(), // empty dir, no hook.yaml
		RunHooks:      false,
	})
	if err == nil {
		t.Fatalf("expected an error for an unresolvable hook workflow reference")
	}
}
