package provenance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The pen's rules (IS6, design §9): the branch grammar, the manifest block
// round-trip, the idempotent trailer, and the preflight verdicts — the
// same findings the IS7 shared rule engine grows from.

func TestBranchGrammar(t *testing.T) {
	if got := BranchName("PAY-T14", "Route the reads!"); got != "orun/PAY-T14-route-the-reads" {
		t.Errorf("BranchName = %s", got)
	}
	cases := map[string]string{
		"orun/PAY-T14-route-the-reads": "PAY-T14",
		"orun/ORN-142-fix":             "ORN-142",
		"orun/WRK-9":                   "WRK-9",
		"agent/ORN-142-fix":            "",
		"orun/lowercase-1-fix":         "",
		"main":                         "",
	}
	for branch, want := range cases {
		if got := TaskKeyOfBranch(branch); got != want {
			t.Errorf("TaskKeyOfBranch(%q) = %q, want %q", branch, got, want)
		}
	}
}

func TestManifestRoundTripAndTrailer(t *testing.T) {
	m := Manifest{Task: "PAY-T14", Epic: "checkout", EpicRevision: "sha256:b2d4",
		Skills: []SkillPin{{Name: "milestone-loop", Rev: "sha256:aa"}}, Session: "as_1"}
	block, err := RenderManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(block, "<!-- orun:manifest") {
		t.Fatalf("block = %q", block[:30])
	}
	body := "## What this is\n\nThe reads.\n\n" + block + "\n"
	parsed, err := ParseManifest(body)
	if err != nil {
		t.Fatal(err)
	}
	if parsed == nil || parsed.Task != "PAY-T14" || parsed.Version != ManifestVersion || parsed.Skills[0].Rev != "sha256:aa" {
		t.Fatalf("parsed = %+v", parsed)
	}
	if p, err := ParseManifest("no block here"); err != nil || p != nil {
		t.Fatalf("absent block: %v %v", p, err)
	}
	if _, err := ParseManifest("<!-- orun:manifest\n{broken"); err == nil {
		t.Fatal("a broken manifest must be a finding, not a silent absence")
	}

	msg := EnsureTrailer("feat: route the reads", "PAY-T14")
	if !strings.HasSuffix(msg, "Orun-Task: PAY-T14\n") {
		t.Fatalf("trailer missing: %q", msg)
	}
	if EnsureTrailer(msg, "PAY-T14") != msg {
		t.Fatal("EnsureTrailer must be idempotent (amends must not stack trailers)")
	}
	if TrailerTaskKey(msg) != "PAY-T14" {
		t.Fatalf("TrailerTaskKey = %q", TrailerTaskKey(msg))
	}
}

func TestVerify(t *testing.T) {
	good := CheckInput{
		Branch:         "orun/PAY-T14-route-the-reads",
		CommitMessages: []string{"feat: reads\n\nOrun-Task: PAY-T14\n"},
		Manifest:       &Manifest{Version: 1, Task: "PAY-T14"},
	}
	if fs := Verify(good); HasErrors(fs) {
		t.Fatalf("clean input found: %+v", fs)
	}

	bad := Verify(CheckInput{
		Branch:         "feature/whatever",
		TaskKey:        "PAY-T14",
		CommitMessages: []string{"feat: no trailer", "fix\n\nOrun-Task: PAY-T15\n"},
		Manifest:       &Manifest{Version: 2, Task: "PAY-T15"},
		HasSkillPins:   true,
	})
	rules := map[string]bool{}
	for _, f := range bad {
		rules[f.Rule+"/"+f.Level] = true
	}
	for _, want := range []string{"branch-grammar/error", "task-trailer/error", "one-task-one-pr/error", "manifest/error", "skill-pins/warn"} {
		if !rules[want] {
			t.Errorf("missing finding %s in %+v", want, bad)
		}
	}
	if !HasErrors(bad) {
		t.Fatal("errors must fail the preflight")
	}
	// No manifest at all: a warning (pr open writes it), never an error.
	warned := Verify(CheckInput{Branch: "orun/PAY-T14"})
	if HasErrors(warned) {
		t.Fatalf("absent manifest must warn, not fail: %+v", warned)
	}
}

// TestPenOpen: the pen ensures the grammar branch, pushes, and — with a
// credential — opens the PR carrying the manifest; without one it hands
// back the compare URL, honestly.
func TestPenOpen(t *testing.T) {
	var gitCalls [][]string
	fakeGit := func(current string) func(ctx context.Context, args ...string) (string, error) {
		return func(_ context.Context, args ...string) (string, error) {
			gitCalls = append(gitCalls, args)
			switch args[0] {
			case "rev-parse":
				return current, nil
			case "remote":
				return "git@github.com:sourceplane/orun.git", nil
			}
			return "", nil
		}
	}

	var posted map[string]interface{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/sourceplane/orun/pulls" {
			t.Errorf("api path = %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&posted)
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"html_url":"https://github.com/sourceplane/orun/pull/7"}`))
	}))
	defer api.Close()

	pen := &Pen{RunGit: fakeGit("main"), Token: func() string { return "tok" }, APIBase: api.URL, HTTP: api.Client()}
	out, err := pen.Open(context.Background(), OpenRequest{
		TaskKey: "PAY-T14", Title: "Route the reads", Draft: true,
		Manifest: Manifest{Version: 1, Skills: []SkillPin{{Name: "milestone-loop", Rev: "sha256:aa"}}, Session: "as_1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Opened || out.URL != "https://github.com/sourceplane/orun/pull/7" {
		t.Fatalf("out = %+v", out)
	}
	if out.Branch != "orun/PAY-T14-route-the-reads" {
		t.Errorf("branch = %s", out.Branch)
	}
	// The branch was created from main (checkout -B) then pushed.
	joined := ""
	for _, c := range gitCalls {
		joined += strings.Join(c, " ") + ";"
	}
	if !strings.Contains(joined, "checkout -B orun/PAY-T14-route-the-reads") || !strings.Contains(joined, "push -u origin orun/PAY-T14-route-the-reads") {
		t.Errorf("git calls = %s", joined)
	}
	// The body carries the manifest, and the manifest names the task.
	body, _ := posted["body"].(string)
	m, perr := ParseManifest(body)
	if perr != nil || m == nil || m.Task != "PAY-T14" || m.Skills[0].Name != "milestone-loop" {
		t.Fatalf("posted manifest = %+v (%v)", m, perr)
	}
	if posted["draft"] != true || posted["base"] != "main" {
		t.Errorf("posted = %+v", posted)
	}

	// Anonymous: prepared, pushed, compare URL — never a fake success.
	gitCalls = nil
	anon := &Pen{RunGit: fakeGit("orun/PAY-T14-already"), Token: func() string { return "" }}
	out, err = anon.Open(context.Background(), OpenRequest{TaskKey: "PAY-T14", Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Opened || out.CompareURL == "" || !out.Pushed {
		t.Fatalf("anonymous out = %+v", out)
	}
	// A branch already on the grammar for this task is kept, not renamed.
	if out.Branch != "orun/PAY-T14-already" {
		t.Errorf("branch = %s", out.Branch)
	}
}
