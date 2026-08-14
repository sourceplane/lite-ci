package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/worklens"
)

func runInitiativesCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := &cobra.Command{Use: "orun", SilenceUsage: true, SilenceErrors: true}
	registerInitiativesAliasCommand(root)
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

// The remote subcommands must resolve a backend + workspace before any wire
// call; in a bare test environment that resolution fails loudly rather than
// silently doing nothing.
func TestInitiativesRemoteCommandsNeedBackend(t *testing.T) {
	for _, args := range [][]string{
		{"initiatives", "list", "--workspace", "ws_test"},
		{"initiatives", "view", "in5", "--workspace", "ws_test"},
		{"initiatives", "create", "--title", "AI-native work", "--workspace", "ws_test"},
		{"initiatives", "task", "view", "WK-1", "--workspace", "ws_test"},
		{"initiatives", "activity", "EP-1", "--workspace", "ws_test"},
		{"initiatives", "doc", "pull", "EP-1", "--workspace", "ws_test"},
		{"initiatives", "design", "list", "in5", "--workspace", "ws_test"},
	} {
		out, err := runInitiativesCmd(t, args...)
		if err == nil {
			t.Errorf("%v succeeded without a backend:\n%s", args, out)
		}
	}
}

// Argument validation fails before any backend resolution.
func TestInitiativesArgumentValidation(t *testing.T) {
	if _, err := runInitiativesCmd(t, "initiatives", "create"); err == nil || !strings.Contains(err.Error(), "--title is required") {
		t.Fatalf("create without --title = %v", err)
	}
	if _, err := runInitiativesCmd(t, "initiatives", "task", "create"); err == nil || !strings.Contains(err.Error(), "--title is required") {
		t.Fatalf("task create without --title = %v", err)
	}
	if _, err := runInitiativesCmd(t, "initiatives", "design", "propose", "in5"); err == nil || !strings.Contains(err.Error(), "--title is required") {
		t.Fatalf("design propose without --title = %v", err)
	}
}

// The importer is shared with the deprecated `orun work` alias; the dry-run
// leg stays fully offline.
func TestInitiativesImportDryRunJSON(t *testing.T) {
	out, err := runInitiativesCmd(t, "initiatives", "import", "../../internal/worklens/testdata/spectree", "--workspace", "ws_test", "--dry-run", "--json")
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

func TestSlugFromTitle(t *testing.T) {
	for in, want := range map[string]string{
		"AI-native work":        "ai-native-work",
		"  Observability 2.0  ": "observability-2-0",
		"---":                   "",
	} {
		if got := slugFromTitle(in); got != want {
			t.Errorf("slugFromTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRungLadderLine(t *testing.T) {
	if got := rungLadderLine("in_review"); got != "draft › ready › in_progress › [in_review] › done › released" {
		t.Fatalf("ladder = %q", got)
	}
	// A rung outside the ladder (canceled) renders bare, never bracketed
	// into a ladder it does not sit on.
	if got := rungLadderLine("canceled"); got != "canceled" {
		t.Fatalf("canceled = %q", got)
	}
}

func TestRenderPortfolio(t *testing.T) {
	p := &remotestate.WorkPortfolio{
		Stats: remotestate.WorkFoldStats{OpenTasks: 5, NeedsYou: 2, AgentsLive: 1},
		Initiatives: []remotestate.WorkPortfolioInitiativeRow{
			{Key: "in5", Title: "Initiatives", Status: "at_risk", TargetDate: "2026-09-01",
				Progress: remotestate.WorkProgressView{Done: 3, Total: 8},
				NeedsYou: []remotestate.WorkNeedsYouReason{{Text: "EP-1 approval drifted"}, {Text: "M2 idle 6d"}}},
			{Key: "obs", Title: "Observability", Status: "planning",
				Progress: remotestate.WorkProgressView{Done: 0, Total: 0}},
		},
	}
	want := "" +
		"KEY  TITLE          STATUS    PROGRESS  NEEDS YOU                   TARGET\n" +
		"in5  Initiatives    at_risk   3/8       EP-1 approval drifted (+1)  2026-09-01\n" +
		"obs  Observability  planning  0/0       -                           -\n" +
		"\nopen tasks: 5 · needs you: 2 · agents live: 1\n"
	if got := renderPortfolio(p); got != want {
		t.Fatalf("portfolio =\n%s\nwant\n%s", got, want)
	}
}

// The view ladder: initiative header, intent chip with @revision + drift,
// milestone states with n/m, rung words with evidence hints, backlog, docs,
// designs — fixture-driven, byte-exact.
func TestRenderInitiativeTree(t *testing.T) {
	tree := &remotestate.WorkInitiativeTree{
		Epics: []remotestate.WorkTreeEpic{{
			Key: "EP-1", Title: "Wire surface",
			Intent: remotestate.WorkEpicIntentView{
				State:      "approved",
				Approval:   &remotestate.WorkApprovalView{Revision: "sha256:b2d4aa00ff11", By: remotestate.WorkActor{Type: "user", ID: "u"}},
				DocDrifted: true,
			},
			Progress: remotestate.WorkProgressView{Done: 3, Active: 2, Total: 8},
			Milestones: []remotestate.WorkTreeMilestone{
				{Key: "M1", Title: "Client", State: "complete", Progress: remotestate.WorkProgressView{Done: 2, Total: 2},
					Tasks: []remotestate.WorkTreeTaskRow{{Key: "WK-1", Title: "types", Rung: "done",
						Evidence: remotestate.WorkTaskEvidenceView{PR: &remotestate.WorkEvidencePR{Number: "7", Merged: true}}}}},
				{Key: "M2", Title: "CLI", State: "active", Progress: remotestate.WorkProgressView{Done: 1, Total: 4},
					Tasks: []remotestate.WorkTreeTaskRow{{Key: "WK-2", Title: "render ladder", Rung: "in_review",
						Evidence: remotestate.WorkTaskEvidenceView{
							Branch: &remotestate.WorkEvidenceBranch{Name: "claude/x"},
							PR:     &remotestate.WorkEvidencePR{Number: "9", ChecksPassed: 3, ChecksTotal: 4}}}}},
			},
			Backlog: []remotestate.WorkTreeTaskRow{{Key: "WK-9", Title: "stretch", Rung: "draft"}},
			Docs: []remotestate.WorkTreeDocRow{{Subject: "EP-1", Kind: "spec", Title: "Wire surface",
				Revision: "sha256:b2d4aa00ff11", State: "drifted"}},
		}},
		Designs: []remotestate.WorkDesignView{{Key: "DSG-1", Title: "Alt", Intent: json.RawMessage(`{"state":"in_review"}`)}},
	}
	tree.Initiative.Key = "in5"
	tree.Initiative.Title = "Initiatives"
	tree.Initiative.Owner = "rahul"
	tree.Initiative.TargetDate = "2026-09-01"
	tree.Initiative.Status = "at_risk"
	tree.Initiative.NeedsYou = []remotestate.WorkNeedsYouReason{{Kind: "approval_drifted", Subject: "EP-1", Text: "EP-1 approval drifted"}}
	tree.Initiative.ProgressView = remotestate.WorkProgressView{Done: 3, Active: 2, Total: 8}
	doc := tree.Epics[0].Docs[0]
	doc.Threads = &struct {
		Total int `json:"total"`
		Open  int `json:"open"`
	}{Total: 3, Open: 1}
	tree.Epics[0].Docs[0] = doc

	want := `in5 — Initiatives  [at_risk]  owner rahul  target 2026-09-01  3/8 done, 2 active
  needs you: EP-1 approval drifted

  EP-1 — Wire surface  intent approved @b2d4aa00 (drifted)  3/8
    M1 — Client  [complete]  2/2
      WK-1       done         types  (PR #7 merged)
    M2 — CLI  [active]  1/4
      WK-2       in_review    render ladder  (branch claude/x; PR #9 open, checks 3/4)
    backlog:
      WK-9       draft        stretch
    doc  spec EP-1 @b2d4aa00 [drifted] — Wire surface  1 open thread(s)

  designs:
    DSG-1 — Alt  [in_review]
`
	if got := renderInitiativeTree(tree); got != want {
		t.Fatalf("tree =\n%s\nwant\n%s", got, want)
	}
}

func TestRenderTaskDetail(t *testing.T) {
	det := &remotestate.WorkTaskDetail{
		Task: remotestate.WorkTaskView{Key: "WK-2", Title: "render ladder",
			Lifecycle: remotestate.WorkLifecycle{Rung: "in_review"}},
		Initiative: &remotestate.WorkItemRef{Key: "in5", Title: "Initiatives"},
		Epic:       &remotestate.WorkItemRef{Key: "EP-1", Title: "Wire surface"},
		Milestone:  &remotestate.WorkItemRef{Key: "M2", Title: "CLI"},
		Evidence: remotestate.WorkTaskEvidenceView{
			Branch: &remotestate.WorkEvidenceBranch{Name: "claude/x", LastPushAt: "2026-08-06T10:00:00Z"},
			PR:     &remotestate.WorkEvidencePR{Number: "9", ChecksPassed: 3, ChecksTotal: 4},
		},
		ComponentsAffected: []remotestate.WorkComponentTouched{{Path: "cmd/orun/initiatives.go", Additions: 120, Deletions: 3}},
		Activity:           []remotestate.WorkActivityEntry{{At: "2026-08-06T10:00:00Z", Text: "opened PR #9"}},
	}
	want := `WK-2 — render ladder
rung  draft › ready › in_progress › [in_review] › done › released
under initiative in5 — Initiatives · epic EP-1 — Wire surface · milestone M2 — CLI

evidence
  branch claude/x  (last push 2026-08-06T10:00:00Z)
  PR #9 open  checks 3/4

components affected
  cmd/orun/initiatives.go  +120 -3

activity
  2026-08-06T10:00:00Z  opened PR #9
`
	if got := renderTaskDetail(det); got != want {
		t.Fatalf("task detail =\n%s\nwant\n%s", got, want)
	}
	// Evidence is never invented: a silent log renders the honest absence.
	bare := &remotestate.WorkTaskDetail{Task: remotestate.WorkTaskView{Key: "WK-3", Title: "quiet",
		Lifecycle: remotestate.WorkLifecycle{Rung: "draft"}}}
	if got := renderTaskDetail(bare); !strings.Contains(got, "(nothing observed yet)") {
		t.Fatalf("bare task detail lacks the honest absence:\n%s", got)
	}
}

func TestRenderActivity(t *testing.T) {
	got := renderActivity(&remotestate.WorkActivity{
		Entries: []remotestate.WorkActivityEntry{
			{At: "2026-08-06T10:00:00Z", Text: "approved EP-1 @b2d4", Tag: "EP-1"},
		},
		NextCursor: "c9",
	})
	want := "2026-08-06T10:00:00Z  approved EP-1 @b2d4  [EP-1]\n\nmore: re-run with --cursor c9\n"
	if got != want {
		t.Fatalf("activity = %q, want %q", got, want)
	}
}
