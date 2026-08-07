package workmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/mcpserve"
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
func (f *fakeAPI) AssignWork(_ context.Context, key, subject string, _ bool) (*remotestate.WorkMutationResponse, error) {
	f.assigned = append(f.assigned, key+"→"+subject)
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
	} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
	if len(tools) != 21 {
		t.Errorf("tool surface = %d tools, want exactly 21 (closed: 13 reads + 8 writes — IN5)", len(tools))
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
