package workmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/mcpserve"
	"github.com/sourceplane/orun/internal/provenance"
	"github.com/sourceplane/orun/internal/remotestate"
)

type fakeAPI struct {
	summary     *remotestate.WorkSummary
	comments    []string
	created     []remotestate.CreateWorkTaskRequest
	assigned    []string
	edited      []string
	designs     []string
	regenerated []string
	brief       *remotestate.WorkEpicBrief
	failNext    error
	// orun-initiatives (IN5)
	initiatives []remotestate.CreateWorkInitiativeRequest
	milestones  []remotestate.WorkMilestoneRequest
	// refuseWrites, when set, is returned by the envelope writes verbatim —
	// the cloud's typed human_only verdict in tests.
	refuseWrites error
	// orun-initiatives-v2 (IS4)
	statusSet []string // key→to
	updates   []string // key: health
	dones     []string // key: note
	notes     []string // key: text
	reviews   []string // collection/key
	verdicts  []string // collection/key=verdict
	approvals []string // key@revision (approve) / key: note (revoke)
	adoptions []string // adopt key / supersede key→by
	// orun-work-spaces (WK4)
	spaces     []string // create prefix / update prefix
	epicStatus []string // key→to (the machine on its right subject)
}

func (f *fakeAPI) ListSpaces(context.Context, bool) (*remotestate.WorkSpaces, error) {
	if f.failNext != nil {
		return nil, f.failNext
	}
	return &remotestate.WorkSpaces{Spaces: []remotestate.WorkSpaceView{{Prefix: "PAY", Title: "Payments", EpicCount: 2}}}, nil
}
func (f *fakeAPI) GetSpace(_ context.Context, prefix string) (*remotestate.WorkSpaceDetail, error) {
	if f.failNext != nil {
		return nil, f.failNext
	}
	d := &remotestate.WorkSpaceDetail{Space: remotestate.WorkSpaceView{Prefix: prefix, Title: "Payments", EpicCount: 1}}
	d.Epics = append(d.Epics, struct {
		Key        string `json:"key"`
		Title      string `json:"title"`
		TargetDate string `json:"targetDate,omitempty"`
	}{Key: "pay-tokens", Title: "Card tokenisation"})
	return d, nil
}
func (f *fakeAPI) CreateSpace(_ context.Context, req remotestate.CreateWorkSpaceRequest) (*remotestate.CreateWorkSpaceResponse, error) {
	if f.refuseWrites != nil {
		return nil, f.refuseWrites
	}
	prefix := req.Prefix
	if prefix == "" {
		prefix = "PAY"
	}
	f.spaces = append(f.spaces, "create "+prefix)
	return &remotestate.CreateWorkSpaceResponse{Key: prefix, Seq: 50, Space: remotestate.WorkSpaceView{Prefix: prefix, Title: req.Title}}, nil
}
func (f *fakeAPI) PatchSpace(_ context.Context, prefix string, _ remotestate.PatchWorkSpaceRequest) (*remotestate.PatchSpaceResponse, error) {
	if f.refuseWrites != nil {
		return nil, f.refuseWrites
	}
	f.spaces = append(f.spaces, "update "+prefix)
	return &remotestate.PatchSpaceResponse{Key: prefix, Seq: 51, Space: remotestate.WorkSpaceView{Prefix: prefix, Title: "Payments"}}, nil
}
func (f *fakeAPI) ListEpics(context.Context, remotestate.WorkEpicsOptions) (*remotestate.WorkEpics, error) {
	if f.failNext != nil {
		return nil, f.failNext
	}
	return &remotestate.WorkEpics{Epics: []remotestate.WorkEpicRow{{Key: "pay-tokens", Title: "Card tokenisation", State: "active", TaskCount: 3}}}, nil
}
func (f *fakeAPI) SetEpicStatus(_ context.Context, key string, req remotestate.SetInitiativeStatusRequest) (*remotestate.SetInitiativeStatusResponse, error) {
	if f.refuseWrites != nil {
		return nil, f.refuseWrites
	}
	f.epicStatus = append(f.epicStatus, key+"→"+req.To)
	out := &remotestate.SetInitiativeStatusResponse{Key: key, Seq: 52, Status: req.To}
	if req.To == "completed" && !req.Force {
		out.Warning = "2 member task(s) still open"
	}
	return out, nil
}
func (f *fakeAPI) PostEpicUpdate(_ context.Context, key string, req remotestate.PostInitiativeUpdateRequest) (*remotestate.PostEpicUpdateResponse, error) {
	if f.refuseWrites != nil {
		return nil, f.refuseWrites
	}
	f.updates = append(f.updates, key+": "+req.Health)
	return &remotestate.PostEpicUpdateResponse{Key: key, Seq: 53, Update: remotestate.WorkEpicUpdateView{
		PublicID: "upd_03", Epic: key, Health: req.Health, Body: req.Body,
		Author: remotestate.WorkActor{Type: "agent", ID: "sp_1"}, CreatedAt: "2026-08-14T00:00:00Z",
	}}, nil
}
func (f *fakeAPI) ListEpicUpdates(_ context.Context, key string) (*remotestate.WorkEpicUpdates, error) {
	if f.failNext != nil {
		return nil, f.failNext
	}
	return &remotestate.WorkEpicUpdates{Updates: []remotestate.WorkEpicUpdateView{{PublicID: "upd_03", Epic: key, Health: "on_track", Body: "steady"}}}, nil
}
func (f *fakeAPI) CreateEpicDesign(_ context.Context, epicKey string, req remotestate.CreateWorkDesignRequest) (*remotestate.WorkMutationResponse, error) {
	if f.refuseWrites != nil {
		return nil, f.refuseWrites
	}
	f.designs = append(f.designs, epicKey+"/"+req.Title)
	return &remotestate.WorkMutationResponse{Key: "PAY-D1", Seq: 54}, nil
}

func (f *fakeAPI) GetWorkSummary(context.Context) (*remotestate.WorkSummary, error) {
	if f.failNext != nil {
		return nil, f.failNext
	}
	return f.summary, nil
}
func (f *fakeAPI) CreateWorkTask(_ context.Context, req remotestate.CreateWorkTaskRequest) (*remotestate.WorkMutationResponse, error) {
	f.created = append(f.created, req)
	return &remotestate.WorkMutationResponse{Key: "ORN-9", Seq: 12}, nil
}
func (f *fakeAPI) CommentWork(_ context.Context, key, body string) (*remotestate.WorkMutationResponse, error) {
	f.comments = append(f.comments, key+": "+body)
	return &remotestate.WorkMutationResponse{Key: key, Seq: 13}, nil
}
func (f *fakeAPI) AssignWorkItem(_ context.Context, key string, req remotestate.AssignWorkItemRequest) (*remotestate.WorkMutationResponse, error) {
	if f.refuseWrites != nil {
		return nil, f.refuseWrites
	}
	arrow := "→"
	if req.Unassign {
		arrow = "⇥"
	}
	f.assigned = append(f.assigned, key+arrow+req.Subject)
	return &remotestate.WorkMutationResponse{Key: key, Seq: 14}, nil
}
func (f *fakeAPI) EditWorkContract(_ context.Context, key string, _ remotestate.WorkContract) (*remotestate.WorkMutationResponse, error) {
	f.edited = append(f.edited, key)
	return &remotestate.WorkMutationResponse{Key: key, Seq: 15}, nil
}
func (f *fakeAPI) GetWorkTimeline(_ context.Context, key string) (*remotestate.WorkTimeline, error) {
	if f.failNext != nil {
		return nil, f.failNext
	}
	return &remotestate.WorkTimeline{Key: key, Entries: []remotestate.WorkTimelineEntry{
		{At: "2026-07-01T00:00:00Z", Type: "event"},
		{At: "2026-07-01T01:00:00Z", Type: "observation"},
	}}, nil
}
func (f *fakeAPI) GetEpicBrief(_ context.Context, epicKey, id string) (*remotestate.WorkEpicBrief, error) {
	if f.failNext != nil {
		return nil, f.failNext
	}
	if f.brief == nil {
		return nil, fmt.Errorf("no sealed brief for %s — approval seals one", epicKey)
	}
	return f.brief, nil
}
func (f *fakeAPI) GetEpicMilestones(_ context.Context, epicKey string) (*remotestate.WorkMilestonesView, error) {
	return &remotestate.WorkMilestonesView{Epic: epicKey, Milestones: []remotestate.WorkMilestoneView{
		{Key: "M1", Title: "Foundation", Goal: "lay it", Ordinal: 0, Total: 2, Complete: 1},
	}}, nil
}
func (f *fakeAPI) GetWorkDesign(_ context.Context, key string) (*remotestate.WorkDesignView, error) {
	return &remotestate.WorkDesignView{Key: key, Initiative: "ai-native-work", Title: "Design One",
		CreatedBy: remotestate.WorkActor{Type: "agent", ID: "sp_1"},
		Intent:    json.RawMessage(`{"state":"draft"}`)}, nil
}
func (f *fakeAPI) GetWorkRollups(_ context.Context, initiativeKey string) (*remotestate.WorkRollups, error) {
	return &remotestate.WorkRollups{Initiative: initiativeKey, Health: "at_risk",
		Evidence: []string{"1 blocked task(s) in demo-epic"}, Progress: map[string]int{"ready": 1},
		Total: 2, Complete: 1, Epics: json.RawMessage(`[]`)}, nil
}
func (f *fakeAPI) CreateWorkDesign(_ context.Context, initiativeKey string, req remotestate.CreateWorkDesignRequest) (*remotestate.WorkMutationResponse, error) {
	f.designs = append(f.designs, initiativeKey+": "+req.Title)
	return &remotestate.WorkMutationResponse{Key: "DSG-1", Seq: 21}, nil
}
func (f *fakeAPI) RegenerateWorkTasks(_ context.Context, epicKey, milestone string, req remotestate.RegenerateWorkTasksRequest) (*remotestate.RegenerateWorkTasksResponse, error) {
	f.regenerated = append(f.regenerated, epicKey+"/"+milestone)
	created := make([]string, 0, len(req.Tasks))
	for i := range req.Tasks {
		created = append(created, fmt.Sprintf("WK-%d", i+1))
	}
	return &remotestate.RegenerateWorkTasksResponse{Canceled: []string{"WK-0"}, Kept: []string{"WK-9"}, Created: created}, nil
}
func (f *fakeAPI) GetWorkDoc(_ context.Context, specKey, rev string) (*remotestate.WorkDoc, error) {
	if f.failNext != nil {
		return nil, f.failNext
	}
	return &remotestate.WorkDoc{Revision: "sha256:aa", SpecKey: specKey, Body: "# Doc\n\nbody for " + specKey + " " + rev}, nil
}
func (f *fakeAPI) ListInitiatives(context.Context) (*remotestate.WorkPortfolio, error) {
	if f.failNext != nil {
		return nil, f.failNext
	}
	return &remotestate.WorkPortfolio{
		Stats: remotestate.WorkFoldStats{OpenTasks: 4, NeedsYou: 1},
		Initiatives: []remotestate.WorkPortfolioInitiativeRow{{
			Key: "ai-native-work", Title: "AI-native work", Status: "at_risk",
			EpicCount:      1,
			Progress:       remotestate.WorkProgressView{Done: 1, Active: 1, Total: 4},
			NeedsYou:       []remotestate.WorkNeedsYouReason{{Kind: "approval_drifted", Subject: "demo-epic", Text: "demo-epic approval drifted"}},
			AgentAssignees: []string{"sp_1"},
			Epics: []remotestate.WorkPortfolioEpicRow{{
				Key: "demo-epic", Title: "Demo",
				Intent:   remotestate.WorkEpicIntentView{State: "approved_drifted", Approval: &remotestate.WorkApprovalView{Revision: "sha256:b2d4", By: remotestate.WorkActor{Type: "user", ID: "u"}}, DocDrifted: true},
				Progress: remotestate.WorkProgressView{Done: 1, Active: 1, Total: 4}, AgentAssignees: []string{"sp_1"},
			}},
			Designs: []remotestate.WorkPortfolioDesignRow{},
		}},
		CoordSeq: 5, ObsSeq: 3,
	}, nil
}
func (f *fakeAPI) GetInitiativeTree(_ context.Context, key string) (*remotestate.WorkInitiativeTree, error) {
	if f.failNext != nil {
		return nil, f.failNext
	}
	tree := &remotestate.WorkInitiativeTree{
		Epics: []remotestate.WorkTreeEpic{{
			Key: "demo-epic", Title: "Demo",
			Intent: remotestate.WorkEpicIntentView{State: "approved", Approval: &remotestate.WorkApprovalView{Revision: "sha256:b2d4", By: remotestate.WorkActor{Type: "user", ID: "u"}}},
			Milestones: []remotestate.WorkTreeMilestone{{
				Key: "M1", Title: "Foundation", State: "active",
				Progress: remotestate.WorkProgressView{Done: 1, Total: 2},
				Tasks: []remotestate.WorkTreeTaskRow{{
					Key: "ORN-1", Title: "route reads", Rung: "in_review",
					Evidence: remotestate.WorkTaskEvidenceView{PR: &remotestate.WorkEvidencePR{Number: "1", Merged: false}},
				}},
			}},
		}},
	}
	tree.Initiative.Key = key
	tree.Initiative.Title = "AI-native work"
	tree.Initiative.CreatedBy = remotestate.WorkActor{Type: "user", ID: "u"}
	tree.Initiative.Status = "on_track"
	tree.Initiative.NeedsYou = []remotestate.WorkNeedsYouReason{}
	tree.Initiative.ProgressView = remotestate.WorkProgressView{Done: 1, Active: 1, Total: 2}
	return tree, nil
}
func (f *fakeAPI) GetTaskDetail(_ context.Context, key string) (*remotestate.WorkTaskDetail, error) {
	if f.failNext != nil {
		return nil, f.failNext
	}
	return &remotestate.WorkTaskDetail{
		Task: remotestate.WorkTaskView{Key: key, Title: "route reads",
			CreatedBy: remotestate.WorkActor{Type: "user", ID: "u"},
			Lifecycle: remotestate.WorkLifecycle{Rung: "in_review", Evidence: []string{"PR o/r#1 open"}}},
		Epic:               &remotestate.WorkItemRef{Key: "demo-epic", Title: "Demo"},
		Evidence:           remotestate.WorkTaskEvidenceView{Branch: &remotestate.WorkEvidenceBranch{Name: "claude/route-reads"}},
		ComponentsAffected: []remotestate.WorkComponentTouched{{Path: "internal/remotestate/work.go", Additions: 12}},
		Activity:           []remotestate.WorkActivityEntry{{At: "2026-08-01T00:00:00Z", Source: "observation", Kind: "pr_opened", Subject: key, Tag: key, Text: "opened PR #1"}},
	}, nil
}
func (f *fakeAPI) GetWorkActivity(_ context.Context, opts remotestate.WorkActivityOptions) (*remotestate.WorkActivity, error) {
	if f.failNext != nil {
		return nil, f.failNext
	}
	return &remotestate.WorkActivity{
		Entries: []remotestate.WorkActivityEntry{{
			At: "2026-08-01T00:00:00Z", Source: "coordination", Kind: "approved",
			Subject: "demo-epic", Tag: opts.Tag, Text: "approved demo-epic @b2d4",
			Actor: &remotestate.WorkActor{Type: "user", ID: "u"},
		}},
		NextCursor: "c2",
	}, nil
}
func (f *fakeAPI) CreateInitiative(_ context.Context, req remotestate.CreateWorkInitiativeRequest) (*remotestate.WorkMutationResponse, error) {
	if f.refuseWrites != nil {
		return nil, f.refuseWrites
	}
	f.initiatives = append(f.initiatives, req)
	return &remotestate.WorkMutationResponse{Key: req.Slug, Seq: 31}, nil
}
func (f *fakeAPI) UpsertMilestones(_ context.Context, epicKey string, req remotestate.WorkMilestoneRequest) (*remotestate.WorkMutationResponse, error) {
	if f.refuseWrites != nil {
		return nil, f.refuseWrites
	}
	f.milestones = append(f.milestones, req)
	return &remotestate.WorkMutationResponse{Key: epicKey + "#" + req.Key, Seq: 32}, nil
}

// ── orun-initiatives-v2 (IS4) ───────────────────────────────────────────────

func (f *fakeAPI) GetWorkItem(_ context.Context, ref string) (*remotestate.WorkItemResolve, error) {
	if f.failNext != nil {
		return nil, f.failNext
	}
	return &remotestate.WorkItemResolve{Kind: "task", Key: ref, CanonicalKey: "PAY-T14", PublicID: "tsk_01ABC", Title: "route reads"}, nil
}
func (f *fakeAPI) GetWorkContext(_ context.Context, ref string, opts remotestate.WorkContextOptions) (*remotestate.WorkContext, error) {
	if f.failNext != nil {
		return nil, f.failNext
	}
	return &remotestate.WorkContext{
		Item: remotestate.WorkContextItem{Kind: "task", Key: ref, CanonicalKey: "PAY-T14", Title: "route reads"},
		View: json.RawMessage(`{"task":{"key":"` + ref + `"}}`),
		Ancestry: []remotestate.WorkContextNode{
			{Kind: "milestone", Key: "demo-epic#M1", CanonicalKey: "PAY-E1#M1", Title: "Foundation", State: "active"},
			{Kind: "initiative", Key: "ai-native-work", CanonicalKey: "PAY", Title: "AI-native work", State: "active"},
		},
		Activity: []remotestate.WorkActivityEntry{{At: "2026-08-01T00:00:00Z", Source: "coordination", Kind: "created", Subject: ref, Tag: ref, Text: "created " + ref}},
		NeedsYou: []remotestate.WorkNeedsYouReason{},
		Budget:   []remotestate.WorkContextBudget{{Level: "tasks", Returned: 50, Total: 90, Cursor: "c1"}},
	}, nil
}
func (f *fakeAPI) GetWorkYours(_ context.Context, opts remotestate.WorkYoursOptions) (*remotestate.WorkYours, error) {
	if f.failNext != nil {
		return nil, f.failNext
	}
	item := remotestate.WorkAttentionItem{
		ID: "att:approval_drifted:demo-epic:usr_dana", Person: "usr_dana",
		Kind: "approval_drifted", Reason: "demo-epic approval drifted",
		Since: "2026-08-10T00:00:00Z", Source: "work",
	}
	item.Subject.Key = "demo-epic"
	item.Subject.Initiative = "ai-native-work"
	item.Act.Tool = "epic_approve"
	item.Act.URL = "/work/items/demo-epic"
	return &remotestate.WorkYours{Items: []remotestate.WorkAttentionItem{item}, Seq: 12}, nil
}
func (f *fakeAPI) GetWorkNow(_ context.Context, opts remotestate.WorkNowOptions) (*remotestate.WorkNow, error) {
	if f.failNext != nil {
		return nil, f.failNext
	}
	return &remotestate.WorkNow{Rows: []remotestate.WorkNowRow{{
		Key: "ORN-1", Title: "route reads", Rung: "in_progress", Seat: "sp_1",
		Now:   &remotestate.WorkNowLine{Text: "tests green, writing the migration", Actor: remotestate.WorkActor{Type: "agent", ID: "sp_1"}, At: "2026-08-13T00:00:00Z"},
		Quiet: false,
	}}, NextCursor: "n2"}, nil
}
func (f *fakeAPI) ListInitiativeUpdates(_ context.Context, key string) (*remotestate.WorkInitiativeUpdates, error) {
	if f.failNext != nil {
		return nil, f.failNext
	}
	return &remotestate.WorkInitiativeUpdates{Updates: []remotestate.WorkInitiativeUpdateView{{
		PublicID: "upd_01", Initiative: key, Health: "at_risk", Body: "checkout epic slipped",
		Author: remotestate.WorkActor{Type: "user", ID: "u"}, CreatedAt: "2026-08-10T00:00:00Z",
	}}}, nil
}
func (f *fakeAPI) SetInitiativeStatus(_ context.Context, key string, req remotestate.SetInitiativeStatusRequest) (*remotestate.SetInitiativeStatusResponse, error) {
	if f.refuseWrites != nil {
		return nil, f.refuseWrites
	}
	f.statusSet = append(f.statusSet, key+"→"+req.To)
	out := &remotestate.SetInitiativeStatusResponse{Key: key, Seq: 41, Status: req.To}
	if req.To == "completed" && !req.Force {
		out.Warning = "2 member task(s) still open"
	}
	return out, nil
}
func (f *fakeAPI) PostInitiativeUpdate(_ context.Context, key string, req remotestate.PostInitiativeUpdateRequest) (*remotestate.PostInitiativeUpdateResponse, error) {
	if f.refuseWrites != nil {
		return nil, f.refuseWrites
	}
	f.updates = append(f.updates, key+": "+req.Health)
	return &remotestate.PostInitiativeUpdateResponse{Key: key, Seq: 42, Update: remotestate.WorkInitiativeUpdateView{
		PublicID: "upd_02", Initiative: key, Health: req.Health, Body: req.Body,
		Author: remotestate.WorkActor{Type: "agent", ID: "sp_1"}, CreatedAt: "2026-08-13T00:00:00Z",
	}}, nil
}
func (f *fakeAPI) AssertTaskDone(_ context.Context, key, note, _ string) (*remotestate.WorkMutationResponse, error) {
	if f.refuseWrites != nil {
		return nil, f.refuseWrites
	}
	f.dones = append(f.dones, key+": "+note)
	return &remotestate.WorkMutationResponse{Key: key, Seq: 43}, nil
}
func (f *fakeAPI) PostTaskNote(_ context.Context, key string, req remotestate.PostTaskNoteRequest) (*remotestate.WorkMutationResponse, error) {
	if f.refuseWrites != nil {
		return nil, f.refuseWrites
	}
	f.notes = append(f.notes, key+": "+req.Text)
	return &remotestate.WorkMutationResponse{Key: key, Seq: 44}, nil
}
func (f *fakeAPI) RequestWorkReview(_ context.Context, collection, key string, req remotestate.WorkReviewRequest) (*remotestate.WorkMutationResponse, error) {
	if f.refuseWrites != nil {
		return nil, f.refuseWrites
	}
	f.reviews = append(f.reviews, collection+"/"+key)
	return &remotestate.WorkMutationResponse{Key: key, Seq: 45}, nil
}
func (f *fakeAPI) SubmitWorkVerdict(_ context.Context, collection, key string, req remotestate.WorkVerdictRequest) (*remotestate.WorkMutationResponse, error) {
	if f.refuseWrites != nil {
		return nil, f.refuseWrites
	}
	f.verdicts = append(f.verdicts, collection+"/"+key+"="+req.Verdict)
	return &remotestate.WorkMutationResponse{Key: key, Seq: 46}, nil
}
func (f *fakeAPI) ApproveEpic(_ context.Context, key string, req remotestate.ApproveEpicRequest) (*remotestate.ApproveEpicResponse, error) {
	if f.refuseWrites != nil {
		return nil, f.refuseWrites
	}
	f.approvals = append(f.approvals, key+"@"+req.Revision)
	return &remotestate.ApproveEpicResponse{Key: key, Seq: 47, Snapshot: "sha256:snap"}, nil
}
func (f *fakeAPI) RevokeEpicApproval(_ context.Context, key, note string) (*remotestate.WorkMutationResponse, error) {
	if f.refuseWrites != nil {
		return nil, f.refuseWrites
	}
	f.approvals = append(f.approvals, key+": "+note)
	return &remotestate.WorkMutationResponse{Key: key, Seq: 48}, nil
}
func (f *fakeAPI) AdoptDesign(_ context.Context, key string, req remotestate.AdoptDesignRequest) (*remotestate.AdoptDesignResponse, error) {
	if f.refuseWrites != nil {
		return nil, f.refuseWrites
	}
	f.adoptions = append(f.adoptions, "adopt "+key)
	return &remotestate.AdoptDesignResponse{Key: key, Seq: 49, Minted: []string{"checkout-epic"}, Tasks: []string{"PAY-T20"}}, nil
}
func (f *fakeAPI) SupersedeDesign(_ context.Context, key string, req remotestate.SupersedeDesignRequest) (*remotestate.WorkMutationResponse, error) {
	if f.refuseWrites != nil {
		return nil, f.refuseWrites
	}
	f.adoptions = append(f.adoptions, "supersede "+key+"→"+req.By)
	return &remotestate.WorkMutationResponse{Key: key, Seq: 50}, nil
}

func fixtureSummary() *remotestate.WorkSummary {
	return &remotestate.WorkSummary{
		Specs: []remotestate.WorkSpecView{{Key: "demo-epic", Title: "Demo", CreatedBy: remotestate.WorkActor{Type: "user", ID: "u"}, Progress: map[string]int{"ready": 1}}},
		Tasks: []remotestate.WorkTaskView{{
			Key: "ORN-1", Spec: "demo-epic", Title: "route reads",
			Contract:  &remotestate.WorkContract{Goal: "g", Affects: []string{"a/b/c"}, DoneWhen: []string{"d"}, Gates: []string{"tests"}},
			CreatedBy: remotestate.WorkActor{Type: "user", ID: "u"},
			Lifecycle: remotestate.WorkLifecycle{Rung: "in_review", Evidence: []string{"PR o/r#1 open"}},
		}},
		CoordSeq: 5, ObsSeq: 3,
	}
}

func rpc(t *testing.T, s *Server, lines ...string) []map[string]interface{} {
	t.Helper()
	// The provider is served exactly as production wires it (cmd/orun/mcp.go):
	// behind the composed mcpserve loop.
	srv := &mcpserve.Server{Providers: []mcpserve.ToolProvider{s}, Version: "test"}
	in := strings.NewReader(strings.Join(lines, "\n") + "\n")
	var out strings.Builder
	if err := srv.Serve(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	var responses []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("bad response line %q: %v", line, err)
		}
		responses = append(responses, m)
	}
	return responses
}

func callLine(id int, tool string, args string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"%s","arguments":%s}}`, id, tool, args)
}

func resultText(t *testing.T, resp map[string]interface{}) (string, bool) {
	t.Helper()
	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("no result in %v", resp)
	}
	content := result["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)
	isErr, _ := result["isError"].(bool)
	return text, isErr
}

func TestInitializeAndToolSurface(t *testing.T) {
	s := &Server{API: &fakeAPI{summary: fixtureSummary()}, Workspace: "ws_1"}
	responses := rpc(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)
	if len(responses) != 2 {
		t.Fatalf("responses = %d (notification must get none)", len(responses))
	}
	init := responses[0]["result"].(map[string]interface{})
	if init["protocolVersion"] != mcpserve.ProtocolVersion {
		t.Fatalf("protocolVersion = %v", init["protocolVersion"])
	}
	// serverInfo is the composed server's: name "orun", binary version (UM0).
	if info := init["serverInfo"].(map[string]interface{}); info["name"] != "orun" {
		t.Fatalf("serverInfo name = %v, want orun", info["name"])
	}

	tools := responses[1]["result"].(map[string]interface{})["tools"].([]interface{})
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.(map[string]interface{})["name"].(string)] = true
	}
	for _, want := range []string{
		"work_query", "work_get", "spec_get", "work_timeline", "spec_doc",
		"task_create", "task_comment", "task_assign", "contract_propose",
		"epic_brief", "milestone_get", "design_get", "initiative_get",
		"design_propose", "task_regenerate",
		"initiatives_list", "initiative_tree", "task_get", "activity_get",
		"initiative_create", "milestone_upsert",
		// orun-initiatives-v2 (IS4/IS2b) — the pen and the queue.
		"work_context", "work_now", "work_yours", "initiative_updates_get",
		"item_assign", "review_request", "review_verdict",
		"task_done", "task_note",
		"initiative_update_post", "initiative_status_set",
		"design_adopt", "design_supersede",
		"epic_approve", "epic_revoke_approval",
		"pr_open",
	} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
	if len(tools) != 45 {
		t.Errorf("tool surface = %d tools, want exactly 45 (21 reads + 24 writes — IS6's 37 plus WK4's Space/epic names beside five IS-era aliases, visible one release then hidden-but-live per WK-6; steady state 40)", len(tools))
	}
	// The lie is unrepresentable: no lifecycle write, no pin (WP-3, WP-10).
	// The sweep runs over the composed server's tools/list (the merged
	// roster — this rpc goes through mcpserve), with the shared fragments.
	for name := range names {
		for _, frag := range mcpserve.ForbiddenNameFragments {
			if strings.Contains(name, frag) {
				t.Errorf("forbidden tool on the surface: %s", name)
			}
		}
	}
}

func TestReadsCarryEvidence(t *testing.T) {
	s := &Server{API: &fakeAPI{summary: fixtureSummary()}, Workspace: "ws_1"}
	responses := rpc(t, s,
		callLine(1, "work_query", `{}`),
		callLine(2, "work_get", `{"key":"ORN-1"}`),
	)
	text, isErr := resultText(t, responses[0])
	if isErr || !strings.Contains(text, "PR o/r#1 open") {
		t.Fatalf("work_query lacks evidence: %s", text)
	}
	text, isErr = resultText(t, responses[1])
	if isErr || !strings.Contains(text, `"rung": "in_review"`) {
		t.Fatalf("work_get lacks the derived rung: %s", text)
	}
}

func TestSpecGetSealsIntentOnly(t *testing.T) {
	s := &Server{API: &fakeAPI{summary: fixtureSummary()}, Workspace: "ws_1"}
	responses := rpc(t, s, callLine(1, "spec_get", `{"spec":"demo-epic"}`))
	text, isErr := resultText(t, responses[0])
	if isErr {
		t.Fatalf("spec_get failed: %s", text)
	}
	if !strings.HasPrefix(text, "sha256:") {
		t.Fatalf("spec_get does not lead with the content id: %s", text[:40])
	}
	if strings.Contains(text, "in_review") || strings.Contains(text, "evidence") {
		t.Fatal("sealed brief leaked fold output")
	}
	if !strings.Contains(text, `"goal":"g"`) {
		t.Fatal("sealed brief lacks the contract")
	}
}

// TestWireAnnotations (UM4): every work tool carries complete, truthful
// annotations — reads are readOnly/non-destructive/idempotent; the write
// tools are non-readOnly, non-destructive (mutator-shaped, WP-6) and NOT
// idempotent (no idempotency key; every call appends a coordination-log
// event — see the Tools() comment). ReadOnly() must agree with the wire.
func TestWireAnnotations(t *testing.T) {
	readNames := map[string]bool{
		"work_query": true, "work_get": true, "spec_get": true,
		"work_timeline": true, "spec_doc": true, "epic_brief": true,
		"milestone_get": true, "design_get": true, "initiative_get": true,
		"initiatives_list": true, "initiative_tree": true, "task_get": true,
		"activity_get": true,
		// IS4 + IS2b
		"work_context": true, "work_now": true, "work_yours": true, "initiative_updates_get": true,
		// orun-work-spaces (WK4)
		"spaces_list": true, "space_get": true, "work_epics": true, "epic_updates_get": true,
	}
	for _, tool := range Tools() {
		want := map[string]bool{
			"readOnlyHint":    readNames[tool.Name],
			"destructiveHint": false,
			"idempotentHint":  readNames[tool.Name],
		}
		for hint, wantVal := range want {
			got, ok := tool.Annotations[hint].(bool)
			if !ok {
				t.Errorf("%s: annotation %s missing or non-bool", tool.Name, hint)
				continue
			}
			if got != wantVal {
				t.Errorf("%s: %s = %v, want %v", tool.Name, hint, got, wantVal)
			}
		}
		if len(tool.Annotations) != 3 {
			t.Errorf("%s: %d annotations, want exactly the 3 hints", tool.Name, len(tool.Annotations))
		}
		if ReadOnly(tool.Name) != readNames[tool.Name] {
			t.Errorf("ReadOnly(%s) disagrees with the wire annotation", tool.Name)
		}
	}
}

func TestReadOnlyV3Tools(t *testing.T) {
	s := &Server{API: &fakeAPI{summary: fixtureSummary()}, Workspace: "ws_1"}
	responses := rpc(t, s,
		callLine(1, "work_timeline", `{"key":"ORN-1"}`),
		callLine(2, "spec_doc", `{"spec":"demo-epic"}`),
		callLine(3, "work_timeline", `{}`),
	)
	text, isErr := resultText(t, responses[0])
	if isErr || !strings.Contains(text, `"observation"`) {
		t.Fatalf("work_timeline lacks the interleaved logs: %s", text)
	}
	text, isErr = resultText(t, responses[1])
	if isErr || !strings.HasPrefix(text, "sha256:aa") || !strings.Contains(text, "demo-epic") {
		t.Fatalf("spec_doc does not lead with the revision digest: %s", text)
	}
	if text, isErr = resultText(t, responses[2]); !isErr || !strings.Contains(text, "key is required") {
		t.Fatalf("work_timeline without a key must fail: %s", text)
	}
}

func TestWritesGoThroughTheMutators(t *testing.T) {
	api := &fakeAPI{summary: fixtureSummary()}
	s := &Server{API: api, Workspace: "ws_1"}
	responses := rpc(t, s,
		callLine(1, "task_create", `{"prefix":"ORN","title":"follow-up","spec":"demo-epic"}`),
		callLine(2, "task_comment", `{"key":"ORN-1","body":"on it"}`),
		callLine(3, "task_assign", `{"key":"ORN-1","subject":"sp_agent"}`),
		callLine(4, "contract_propose", `{"key":"ORN-1","contract":{"goal":"g2"}}`),
	)
	for i, r := range responses {
		if _, isErr := resultText(t, r); isErr {
			t.Fatalf("write %d errored: %v", i+1, r)
		}
	}
	if len(api.created) != 1 || api.created[0].Title != "follow-up" {
		t.Fatalf("created = %+v", api.created)
	}
	if len(api.assigned) != 1 || api.assigned[0] != "ORN-1→sp_agent" {
		t.Fatalf("assigned = %v", api.assigned)
	}
	if len(api.edited) != 1 {
		t.Fatalf("edited = %v", api.edited)
	}
	// contract_propose flags for human review (comment beside the edit)
	flagged := false
	for _, c := range api.comments {
		if strings.Contains(c, "human review requested") {
			flagged = true
		}
	}
	if !flagged {
		t.Fatalf("proposal not flagged: %v", api.comments)
	}
}

func TestErrorShapes(t *testing.T) {
	s := &Server{API: &fakeAPI{summary: fixtureSummary(), failNext: fmt.Errorf("backend down")}, Workspace: "ws_1"}
	responses := rpc(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"no/such"}`,
		callLine(2, "work_query", `{}`),
		callLine(3, "no_such_tool", `{}`),
	)
	errObj := responses[0]["error"].(map[string]interface{})
	if errObj["code"].(float64) != -32601 {
		t.Fatalf("unknown method code = %v", errObj["code"])
	}
	// Tool failures are results with isError (verdicts to reason about),
	// never protocol faults.
	text, isErr := resultText(t, responses[1])
	if !isErr || !strings.Contains(text, "backend down") {
		t.Fatalf("tool failure shape: %s (isError=%v)", text, isErr)
	}
	text, isErr = resultText(t, responses[2])
	if !isErr || !strings.Contains(text, "unknown tool") {
		t.Fatalf("unknown tool shape: %s", text)
	}
}

// TestInitiativeSurfaceReads (IN5): the four new folds come back as JSON
// with the derived fields intact — status words, needs-you reasons, rungs
// with evidence, and the tagged tail's server sentences.
func TestInitiativeSurfaceReads(t *testing.T) {
	s := &Server{API: &fakeAPI{summary: fixtureSummary()}, Workspace: "ws_1"}
	responses := rpc(t, s,
		callLine(1, "initiatives_list", `{}`),
		callLine(2, "initiative_tree", `{"key":"ai-native-work"}`),
		callLine(3, "task_get", `{"key":"ORN-1"}`),
		callLine(4, "activity_get", `{"tag":"demo-epic","limit":10}`),
		callLine(5, "initiative_tree", `{}`),
		callLine(6, "task_get", `{}`),
	)
	text, isErr := resultText(t, responses[0])
	if isErr || !strings.Contains(text, `"status": "at_risk"`) || !strings.Contains(text, "approval drifted") {
		t.Fatalf("initiatives_list lacks the derived portfolio: %s", text)
	}
	if !strings.Contains(text, `"openTasks": 4`) {
		t.Fatalf("initiatives_list lacks fold-stats: %s", text)
	}
	text, isErr = resultText(t, responses[1])
	if isErr || !strings.Contains(text, `"state": "approved"`) || !strings.Contains(text, `"rung": "in_review"`) {
		t.Fatalf("initiative_tree lacks intent + rungs: %s", text)
	}
	if !strings.Contains(text, `"progressView"`) {
		t.Fatalf("initiative_tree lacks the progress fold: %s", text)
	}
	text, isErr = resultText(t, responses[2])
	if isErr || !strings.Contains(text, `"branch"`) || !strings.Contains(text, "opened PR #1") {
		t.Fatalf("task_get lacks evidence + activity: %s", text)
	}
	text, isErr = resultText(t, responses[3])
	if isErr || !strings.Contains(text, "approved demo-epic @b2d4") || !strings.Contains(text, `"nextCursor": "c2"`) {
		t.Fatalf("activity_get lacks the tagged tail: %s", text)
	}
	if text, isErr = resultText(t, responses[4]); !isErr || !strings.Contains(text, "key is required") {
		t.Fatalf("initiative_tree without a key must fail: %s", text)
	}
	if text, isErr = resultText(t, responses[5]); !isErr || !strings.Contains(text, "key is required") {
		t.Fatalf("task_get without a key must fail: %s", text)
	}
}

// TestInitiativeSurfaceWrites (IN5): the two envelope writes go through the
// mutators with their arguments intact.
func TestInitiativeSurfaceWrites(t *testing.T) {
	api := &fakeAPI{summary: fixtureSummary()}
	s := &Server{API: api, Workspace: "ws_1"}
	responses := rpc(t, s,
		callLine(1, "initiative_create", `{"slug":"ai-native-work","title":"AI-native work","successCriteria":["agents ship epics"]}`),
		callLine(2, "milestone_upsert", `{"epic":"demo-epic","op":"reorder","key":"M2","ordinal":0}`),
		callLine(3, "initiative_create", `{"title":"missing slug"}`),
		callLine(4, "milestone_upsert", `{"epic":"demo-epic","key":"M2"}`),
	)
	text, isErr := resultText(t, responses[0])
	if isErr || !strings.Contains(text, "created initiative ai-native-work (event seq 31)") {
		t.Fatalf("initiative_create result: %s", text)
	}
	if len(api.initiatives) != 1 || api.initiatives[0].SuccessCriteria[0] != "agents ship epics" {
		t.Fatalf("initiatives = %+v", api.initiatives)
	}
	text, isErr = resultText(t, responses[1])
	if isErr || !strings.Contains(text, "applied reorder to milestone M2 on demo-epic") {
		t.Fatalf("milestone_upsert result: %s", text)
	}
	// Ordinal 0 must survive the wire (reorder to the top of the ladder).
	if len(api.milestones) != 1 || api.milestones[0].Ordinal == nil || *api.milestones[0].Ordinal != 0 {
		t.Fatalf("milestones = %+v", api.milestones)
	}
	if text, isErr = resultText(t, responses[2]); !isErr || !strings.Contains(text, "slug and title are required") {
		t.Fatalf("initiative_create without a slug must fail: %s", text)
	}
	if text, isErr = resultText(t, responses[3]); !isErr || !strings.Contains(text, "epic, op, and key are required") {
		t.Fatalf("milestone_upsert without an op must fail: %s", text)
	}
}

// TestHumanOnlyRefusalPassesThrough (IN-4): when a write brushes a human-only
// decision, the cloud answers with the typed WorkError("human_only", …) and
// the MCP layer surfaces it VERBATIM — code included — so the model can tell
// "not allowed for you" from "does not exist". The refusal is an isError
// result (a verdict to reason about), never a protocol fault.
func TestHumanOnlyRefusalPassesThrough(t *testing.T) {
	refusal := &remotestate.APIError{
		Code:    "human_only",
		Message: "approving an epic is a human decision",
		Status:  403,
	}
	s := &Server{API: &fakeAPI{summary: fixtureSummary(), refuseWrites: refusal}, Workspace: "ws_1"}
	responses := rpc(t, s,
		callLine(1, "milestone_upsert", `{"epic":"demo-epic","op":"edit","key":"M1","title":"x"}`),
		callLine(2, "initiative_create", `{"slug":"x","title":"X"}`),
		// IS4: the decisions are nameable now, and the refusal still passes
		// through VERBATIM when a non-human actor reaches the model layer —
		// naming a decision is not deciding.
		callLine(3, "epic_approve", `{"key":"demo-epic","revision":"sha256:b2d4"}`),
		callLine(4, "design_adopt", `{"key":"DSG-1"}`),
		callLine(5, "initiative_status_set", `{"key":"ai-native-work","to":"completed"}`),
	)
	for i, r := range responses {
		text, isErr := resultText(t, r)
		if !isErr {
			t.Fatalf("refusal %d not an isError result: %s", i+1, text)
		}
		if !strings.Contains(text, "human_only") {
			t.Fatalf("refusal %d dropped the typed code: %s", i+1, text)
		}
		if !strings.Contains(text, "approving an epic is a human decision") {
			t.Fatalf("refusal %d dropped the server message: %s", i+1, text)
		}
	}
}

// TestIS4Reads: the context bundle comes back whole (view, ancestry with
// live states, the budget echo — no silent caps), the live board carries the
// now lines, and the update feed carries attributed health headlines.
func TestIS4Reads(t *testing.T) {
	s := &Server{API: &fakeAPI{summary: fixtureSummary()}, Workspace: "ws_1"}
	responses := rpc(t, s,
		callLine(1, "work_context", `{"key":"PAY-14","depth":3}`),
		callLine(2, "work_now", `{"seat":"sp_1"}`),
		callLine(3, "initiative_updates_get", `{"key":"ai-native-work"}`),
		callLine(4, "work_context", `{}`),
		callLine(5, "initiative_updates_get", `{}`),
		callLine(6, "work_yours", `{"after":5,"waitSeconds":10}`),
	)
	text, isErr := resultText(t, responses[0])
	if isErr || !strings.Contains(text, `"canonicalKey": "PAY-T14"`) {
		t.Fatalf("work_context lacks the resolved item: %s", text)
	}
	if !strings.Contains(text, `"ancestry"`) || !strings.Contains(text, `"state": "active"`) {
		t.Fatalf("work_context lacks ancestry with live states: %s", text)
	}
	if !strings.Contains(text, `"returned": 50`) || !strings.Contains(text, `"total": 90`) {
		t.Fatalf("work_context lacks the budget echo (IS-H): %s", text)
	}
	text, isErr = resultText(t, responses[1])
	if isErr || !strings.Contains(text, "tests green, writing the migration") || !strings.Contains(text, `"nextCursor": "n2"`) {
		t.Fatalf("work_now lacks the live board: %s", text)
	}
	text, isErr = resultText(t, responses[2])
	if isErr || !strings.Contains(text, `"health": "at_risk"`) || !strings.Contains(text, "checkout epic slipped") {
		t.Fatalf("initiative_updates_get lacks the feed: %s", text)
	}
	if text, isErr = resultText(t, responses[3]); !isErr || !strings.Contains(text, "key is required") {
		t.Fatalf("work_context without a key must fail: %s", text)
	}
	if text, isErr = resultText(t, responses[4]); !isErr || !strings.Contains(text, "key is required") {
		t.Fatalf("initiative_updates_get without a key must fail: %s", text)
	}
	// IS2b: the addressed queue — every item carries its person and the one
	// gesture that clears it, and the seq watermark rides the response.
	text, isErr = resultText(t, responses[5])
	if isErr || !strings.Contains(text, `"person": "usr_dana"`) || !strings.Contains(text, `"tool": "epic_approve"`) {
		t.Fatalf("work_yours lacks the addressed queue: %s", text)
	}
	if !strings.Contains(text, `"seq": 12`) {
		t.Fatalf("work_yours lacks the long-poll watermark: %s", text)
	}
}

// TestIS4VoiceAndAssign: the assertion lane demands its note, the worklog
// flows, the generalized assign covers assign and unassign, and task_assign
// (absorbed, never renamed) forwards to the same generalized path.
func TestIS4VoiceAndAssign(t *testing.T) {
	api := &fakeAPI{summary: fixtureSummary()}
	s := &Server{API: api, Workspace: "ws_1"}
	responses := rpc(t, s,
		callLine(1, "task_done", `{"key":"ORN-1","note":"docs shipped; no PR will exist"}`),
		callLine(2, "task_done", `{"key":"ORN-1"}`),
		callLine(3, "task_note", `{"key":"ORN-1","text":"tests green, writing the migration","ref":"abc123"}`),
		callLine(4, "item_assign", `{"key":"demo-epic","subject":"usr_7"}`),
		callLine(5, "item_assign", `{"key":"ORN-1","subject":"sp_1","unassign":true}`),
		callLine(6, "task_assign", `{"key":"ORN-1","subject":"sp_agent"}`),
	)
	text, isErr := resultText(t, responses[0])
	if isErr || !strings.Contains(text, "done asserted on ORN-1") || !strings.Contains(text, "weakest voice") {
		t.Fatalf("task_done result: %s", text)
	}
	if text, isErr = resultText(t, responses[1]); !isErr || !strings.Contains(text, "note are required") {
		t.Fatalf("task_done without a note must fail — an assertion without a reason is a status write: %s", text)
	}
	text, isErr = resultText(t, responses[2])
	if isErr || !strings.Contains(text, "narration is inert") {
		t.Fatalf("task_note result: %s", text)
	}
	if len(api.dones) != 1 || api.dones[0] != "ORN-1: docs shipped; no PR will exist" {
		t.Fatalf("dones = %v", api.dones)
	}
	if len(api.notes) != 1 || api.notes[0] != "ORN-1: tests green, writing the migration" {
		t.Fatalf("notes = %v", api.notes)
	}
	text, isErr = resultText(t, responses[3])
	if isErr || !strings.Contains(text, "assigned demo-epic ↔ usr_7") {
		t.Fatalf("item_assign result: %s", text)
	}
	text, isErr = resultText(t, responses[4])
	if isErr || !strings.Contains(text, "unassigned") {
		t.Fatalf("item_assign unassign result: %s", text)
	}
	if _, isErr = resultText(t, responses[5]); isErr {
		t.Fatalf("task_assign (absorbed) errored: %v", responses[5])
	}
	// All three assigns — including forwarded task_assign — went through the
	// ONE generalized path.
	if len(api.assigned) != 3 || api.assigned[2] != "ORN-1→sp_agent" {
		t.Fatalf("assigned = %v", api.assigned)
	}
}

// TestIS4StateAndDecisions: the state machine speaks (warning surfaced),
// updates post attributed, reviews route by key grammar (designs vs epics),
// verdicts demand their reasoning, and the four signature tools reach the
// mutators with their arguments intact.
func TestIS4StateAndDecisions(t *testing.T) {
	api := &fakeAPI{summary: fixtureSummary()}
	s := &Server{API: api, Workspace: "ws_1"}
	responses := rpc(t, s,
		callLine(1, "initiative_status_set", `{"key":"ai-native-work","to":"active"}`),
		callLine(2, "initiative_status_set", `{"key":"ai-native-work","to":"completed"}`),
		callLine(3, "initiative_update_post", `{"key":"ai-native-work","health":"on_track","body":"checkout epic landed"}`),
		callLine(4, "review_request", `{"key":"DSG-1","note":"compare with option B"}`),
		callLine(5, "review_request", `{"key":"demo-epic"}`),
		callLine(6, "review_verdict", `{"key":"PAY-D3","verdict":"approve","note":"model is sound"}`),
		callLine(7, "review_verdict", `{"key":"demo-epic","verdict":"approve"}`),
		callLine(8, "epic_approve", `{"key":"demo-epic","revision":"sha256:b2d4"}`),
		callLine(9, "epic_approve", `{"key":"demo-epic"}`),
		callLine(10, "epic_revoke_approval", `{"key":"demo-epic","note":"scope changed"}`),
		callLine(11, "design_adopt", `{"key":"DSG-1","taskPrefix":"PAY"}`),
		callLine(12, "design_supersede", `{"key":"DSG-1","by":"DSG-2"}`),
	)
	text, isErr := resultText(t, responses[0])
	if isErr || !strings.Contains(text, "ai-native-work is now active") {
		t.Fatalf("initiative_status_set result: %s", text)
	}
	// The complete-warn surfaces beside the success — never blocks (IS2).
	text, isErr = resultText(t, responses[1])
	if isErr || !strings.Contains(text, "2 member task(s) still open") {
		t.Fatalf("complete warning not surfaced: %s", text)
	}
	text, isErr = resultText(t, responses[2])
	if isErr || !strings.Contains(text, "health headline now on_track") {
		t.Fatalf("initiative_update_post result: %s", text)
	}
	if _, isErr = resultText(t, responses[3]); isErr {
		t.Fatalf("review_request design errored: %v", responses[3])
	}
	if _, isErr = resultText(t, responses[4]); isErr {
		t.Fatalf("review_request epic errored: %v", responses[4])
	}
	// The collection heuristic: design keys (legacy DSG-n, typed PFX-Dn)
	// ride /designs; slugs ride /epics.
	if len(api.reviews) != 2 || api.reviews[0] != "designs/DSG-1" || api.reviews[1] != "epics/demo-epic" {
		t.Fatalf("reviews = %v", api.reviews)
	}
	if _, isErr = resultText(t, responses[5]); isErr {
		t.Fatalf("review_verdict errored: %v", responses[5])
	}
	if len(api.verdicts) != 1 || api.verdicts[0] != "designs/PAY-D3=approve" {
		t.Fatalf("verdicts = %v", api.verdicts)
	}
	if text, isErr = resultText(t, responses[6]); !isErr || !strings.Contains(text, "note are required") {
		t.Fatalf("review_verdict without a note must fail — a vote, not a review: %s", text)
	}
	text, isErr = resultText(t, responses[7])
	if isErr || !strings.Contains(text, "sealed brief sha256:snap") {
		t.Fatalf("epic_approve result: %s", text)
	}
	if text, isErr = resultText(t, responses[8]); !isErr || !strings.Contains(text, "revision are required") {
		t.Fatalf("epic_approve without a revision must fail: %s", text)
	}
	if _, isErr = resultText(t, responses[9]); isErr {
		t.Fatalf("epic_revoke_approval errored: %v", responses[9])
	}
	// WK3 (§4.2): adoption mints the ladder INSIDE the design's own epic
	// and drifts its approval by derivation — no epic is minted, no rev-0
	// approval rides the signature anymore.
	text, isErr = resultText(t, responses[10])
	if isErr || !strings.Contains(text, "minted milestones [checkout-epic]") || !strings.Contains(text, "approval drifts until re-approved") {
		t.Fatalf("design_adopt result: %s", text)
	}
	if _, isErr = resultText(t, responses[11]); isErr {
		t.Fatalf("design_supersede errored: %v", responses[11])
	}
	if len(api.adoptions) != 2 || api.adoptions[0] != "adopt DSG-1" || api.adoptions[1] != "supersede DSG-1→DSG-2" {
		t.Fatalf("adoptions = %v", api.adoptions)
	}
	if len(api.approvals) != 2 || api.approvals[0] != "demo-epic@sha256:b2d4" || api.approvals[1] != "demo-epic: scope changed" {
		t.Fatalf("approvals = %v", api.approvals)
	}
	if len(api.statusSet) != 2 || api.statusSet[0] != "ai-native-work→active" {
		t.Fatalf("statusSet = %v", api.statusSet)
	}
	if len(api.updates) != 1 || api.updates[0] != "ai-native-work: on_track" {
		t.Fatalf("updates = %v", api.updates)
	}
}

// fakePen records pr_open gestures and answers like the honest pen.
type fakePen struct {
	opened []provenance.OpenRequest
	url    string // "" = anonymous (compare-URL fallback)
}

func (f *fakePen) Open(_ context.Context, req provenance.OpenRequest) (*provenance.OpenResult, error) {
	f.opened = append(f.opened, req)
	out := &provenance.OpenResult{Branch: "orun/" + req.TaskKey + "-x", Pushed: true, Body: "body"}
	if f.url != "" {
		out.Opened = true
		out.URL = f.url
	} else {
		out.CompareURL = "https://github.com/o/r/compare/main...orun/" + req.TaskKey + "-x?expand=1"
	}
	return out, nil
}

// TestPrOpen (IS6): the pen completes the roster — mounted, it opens (or
// honestly prepares) the task's PR; unmounted, the verdict says exactly
// what to do instead. task stays required: a PR opens FOR a task.
func TestPrOpen(t *testing.T) {
	pen := &fakePen{url: "https://github.com/o/r/pull/7"}
	s := &Server{API: &fakeAPI{summary: fixtureSummary()}, Workspace: "ws_1", Pen: pen}
	responses := rpc(t, s,
		callLine(1, "pr_open", `{"task":"PAY-T14","title":"Route the reads","draft":true}`),
		callLine(2, "pr_open", `{}`),
	)
	text, isErr := resultText(t, responses[0])
	if isErr || !strings.Contains(text, "opened https://github.com/o/r/pull/7") {
		t.Fatalf("pr_open result: %s", text)
	}
	if len(pen.opened) != 1 || pen.opened[0].TaskKey != "PAY-T14" || !pen.opened[0].Draft {
		t.Fatalf("pen gestures = %+v", pen.opened)
	}
	if text, isErr = resultText(t, responses[1]); !isErr || !strings.Contains(text, "task is required") {
		t.Fatalf("pr_open without a task must fail: %s", text)
	}

	// Anonymous pen: the compare URL comes back — prepared, never faked.
	anon := &Server{API: &fakeAPI{summary: fixtureSummary()}, Workspace: "ws_1", Pen: &fakePen{}}
	responses = rpc(t, anon, callLine(1, "pr_open", `{"task":"PAY-T14"}`))
	if text, isErr = resultText(t, responses[0]); isErr || !strings.Contains(text, "compare/main...orun/PAY-T14") {
		t.Fatalf("anonymous pr_open result: %s", text)
	}

	// No pen mounted (a repo-less serve): a clear verdict, not a git guess.
	bare := &Server{API: &fakeAPI{summary: fixtureSummary()}, Workspace: "ws_1"}
	responses = rpc(t, bare, callLine(1, "pr_open", `{"task":"PAY-T14"}`))
	if text, isErr = resultText(t, responses[0]); !isErr || !strings.Contains(text, "no repository workspace mounted") {
		t.Fatalf("penless pr_open verdict: %s", text)
	}
}
