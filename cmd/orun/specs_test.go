package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The D1 CLI seam: 'spec push' uploads the COMMITTED copy with a pointer
// whose sha always describes the bytes — working-tree edits ride a warning,
// never the payload — and 'spec list' renders the pointers and seals.

// initSpecRepo builds a real git repo with one committed spec file and
// returns (repoRoot, specPath, headSha).
func initSpecRepo(t *testing.T) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	run("remote", "add", "origin", "https://github.com/acme/web.git")
	if err := os.MkdirAll(filepath.Join(dir, "specs", "payments"), 0o755); err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(dir, "specs", "payments", "Design Notes.md")
	if err := os.WriteFile(specPath, []byte("# The Payments Design\n\nThe plan.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "spec")
	sha := run("rev-parse", "HEAD")
	t.Chdir(dir)
	return dir, specPath, sha
}

func TestSpecPushSendsCommittedCopyWithPointer(t *testing.T) {
	_, specPath, sha := initSpecRepo(t)

	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/organizations/org_x/tasks/epics/payments/docs/design-notes" {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("decode push body: %v", err)
		}
		envelope(t, w, 200, map[string]any{"updated": true, "contentHash": "sha256:" + strings.Repeat("ab", 32)})
	}))
	defer srv.Close()

	out, err := runTaskCmd(t, newSpecPushCommand(), srv.URL, specPath, "--epic", "payments")
	if err != nil {
		t.Fatalf("spec push: %v\n%s", err, out)
	}
	if got["repo"] != "acme/web" || got["path"] != "specs/payments/Design Notes.md" {
		t.Fatalf("pointer wrong: %v", got)
	}
	if got["sha"] != sha {
		t.Fatalf("sha %v is not HEAD %s", got["sha"], sha)
	}
	if got["title"] != "The Payments Design" {
		t.Fatalf("title not derived from the heading: %v", got["title"])
	}
	if !strings.Contains(got["content"].(string), "The plan.") {
		t.Fatalf("content missing: %v", got["content"])
	}
	if !strings.Contains(out, "pushed design-notes → payments") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestSpecPushIsHonestAboutDirtyFilesAndUncommittedOnes(t *testing.T) {
	_, specPath, _ := initSpecRepo(t)
	// Dirty: the committed copy is pushed and the note says so.
	if err := os.WriteFile(specPath, []byte("# Edited but not committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var pushedContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		pushedContent, _ = body["content"].(string)
		envelope(t, w, 200, map[string]any{"updated": true, "contentHash": "sha256:" + strings.Repeat("ab", 32)})
	}))
	defer srv.Close()

	out, err := runTaskCmd(t, newSpecPushCommand(), srv.URL, specPath, "--epic", "payments")
	if err != nil {
		t.Fatalf("spec push (dirty): %v\n%s", err, out)
	}
	if !strings.Contains(pushedContent, "The plan.") || strings.Contains(pushedContent, "Edited but not committed") {
		t.Fatalf("pushed the working copy, not the committed one:\n%s", pushedContent)
	}
	if !strings.Contains(out, "working-tree changes") {
		t.Fatalf("no dirty note:\n%s", out)
	}

	// Never committed at all: refused with the reason, before any request.
	fresh := filepath.Join(filepath.Dir(specPath), "brand-new.md")
	if err := os.WriteFile(fresh, []byte("# New\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = runTaskCmd(t, newSpecPushCommand(), srv.URL, fresh, "--epic", "payments")
	if err == nil || !strings.Contains(err.Error(), "not committed at HEAD") {
		t.Fatalf("expected the commit-first refusal, got err=%v\n%s", err, out)
	}
}

func TestSpecPushUnchangedReadsAsANoop(t *testing.T) {
	_, specPath, _ := initSpecRepo(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		envelope(t, w, 200, map[string]any{"updated": false, "contentHash": "sha256:" + strings.Repeat("ab", 32)})
	}))
	defer srv.Close()

	out, err := runTaskCmd(t, newSpecPushCommand(), srv.URL, specPath, "--epic", "payments")
	if err != nil {
		t.Fatalf("spec push: %v\n%s", err, out)
	}
	if !strings.Contains(out, "unchanged design-notes") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestSpecPushRequiresEpic(t *testing.T) {
	_, specPath, _ := initSpecRepo(t)
	out, err := runTaskCmd(t, newSpecPushCommand(), "", specPath)
	if err == nil || !strings.Contains(err.Error(), "--epic is required") {
		t.Fatalf("expected the --epic refusal, got err=%v\n%s", err, out)
	}
}

func TestSpecListRendersPointersAndSeals(t *testing.T) {
	initSpecRepo(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/organizations/org_x/tasks/epics/payments/docs" {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
		envelope(t, w, 200, map[string]any{"docs": []map[string]any{
			{
				"slug": "design-notes", "title": "The Payments Design", "repo": "acme/web",
				"path": "specs/payments/Design Notes.md", "gitSha": strings.Repeat("a", 40),
				"contentHash": "sha256:" + strings.Repeat("ab", 32), "pushedBy": "usr_1",
				"pushedAt": "2026-08-27T00:00:00Z", "sizeBytes": 42,
			},
		}})
	}))
	defer srv.Close()

	out, err := runTaskCmd(t, newSpecListCommand(), srv.URL, "--epic", "payments")
	if err != nil {
		t.Fatalf("spec list: %v\n%s", err, out)
	}
	for _, want := range []string{"design-notes", "The Payments Design", "acme/web/specs/payments/Design Notes.md", "aaaaaaa"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list output missing %q:\n%s", want, out)
		}
	}
}

func TestSpecSlugDerivation(t *testing.T) {
	for in, want := range map[string]string{
		"specs/Design Notes.md": "design-notes",
		"ADR_001 (draft).md":    "adr-001-draft",
		"weird///.md":           "",
	} {
		if got := specSlugFor(in); got != want {
			t.Errorf("specSlugFor(%q) = %q, want %q", in, got, want)
		}
	}
}
