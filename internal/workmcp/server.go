// Package workmcp is the work-plane tool provider of the orun MCP
// (orun-work v2 WP5, mounted by orun-mcp UM0): the agent surface,
// policy-identical to the console. The stdio JSON-RPC transport lives in
// internal/mcpserve; this package supplies the tools.
//
// The tool surface is the whole point (agents-and-mcp.md): reads return the
// fold's output WITH evidence; the write surface is mutator-shaped — the v2
// four (task_create, task_comment, task_assign, contract_propose), the v4
// pair (design_propose, task_regenerate), the orun-initiatives pair
// (initiative_create, milestone_upsert), and the orun-initiatives-v2 (IS4)
// growth: the context bundle + live board reads, the stored initiative
// speech acts, the agent's voice (task_done / task_note), the generalized
// item_assign, and — for the first time — the DECISIONS as nameable tools
// (review_request, review_verdict, epic_approve, epic_revoke_approval,
// design_adopt, design_supersede). Naming a decision is not deciding: the
// tier matrix (agents/*.md allow/ask/deny) routes signature-shaped tools
// through the ask lane, where a human confirmation is the signature and an
// unattended session auto-denies; the cloud's model layer refuses non-human
// actors again server-side (defense in depth, not client-side trust).
//
// Still absent, forever: a task-rung write (lifecycle is a derived query,
// WP-3 narrowed — no stored DELIVERY status; the category "agent lies about
// status" stays unrepresentable) and a pin tool (human-only, WP-10). When a
// write brushes a human-only rule, the cloud's typed WorkError("human_only",
// …) verdict surfaces verbatim so the model can tell "not allowed for you"
// from "does not exist" (IN-4).
package workmcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sourceplane/orun/internal/mcpserve"
	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/workbrief"
	"github.com/sourceplane/orun/internal/worklens"
)

// WorkAPI is the seam onto the cloud work plane; *remotestate.Client
// implements it. Every write goes through the same mutators as the console
// keyboard (WD/WP one-write-path heritage).
type WorkAPI interface {
	GetWorkSummary(ctx context.Context) (*remotestate.WorkSummary, error)
	GetWorkTimeline(ctx context.Context, key string) (*remotestate.WorkTimeline, error)
	GetWorkDoc(ctx context.Context, specKey, rev string) (*remotestate.WorkDoc, error)
	CreateWorkTask(ctx context.Context, req remotestate.CreateWorkTaskRequest) (*remotestate.WorkMutationResponse, error)
	CommentWork(ctx context.Context, key, body string) (*remotestate.WorkMutationResponse, error)
	EditWorkContract(ctx context.Context, key string, contract remotestate.WorkContract) (*remotestate.WorkMutationResponse, error)
	// v4 (WH5) — the hierarchy legs. Reads only for decisions: there is no
	// approve/adopt call on this seam at all (V4-2).
	GetEpicBrief(ctx context.Context, epicKey, id string) (*remotestate.WorkEpicBrief, error)
	GetEpicMilestones(ctx context.Context, epicKey string) (*remotestate.WorkMilestonesView, error)
	GetWorkDesign(ctx context.Context, key string) (*remotestate.WorkDesignView, error)
	GetWorkRollups(ctx context.Context, initiativeKey string) (*remotestate.WorkRollups, error)
	CreateWorkDesign(ctx context.Context, initiativeKey string, req remotestate.CreateWorkDesignRequest) (*remotestate.WorkMutationResponse, error)
	RegenerateWorkTasks(ctx context.Context, epicKey, milestone string, req remotestate.RegenerateWorkTasksRequest) (*remotestate.RegenerateWorkTasksResponse, error)
	// orun-initiatives (IN5) — the four derived folds the Initiatives
	// surface renders, plus the two envelope writes. Decisions stay off
	// this seam entirely (human_only verdicts come back typed).
	ListInitiatives(ctx context.Context) (*remotestate.WorkPortfolio, error)
	GetInitiativeTree(ctx context.Context, key string) (*remotestate.WorkInitiativeTree, error)
	GetTaskDetail(ctx context.Context, key string) (*remotestate.WorkTaskDetail, error)
	GetWorkActivity(ctx context.Context, opts remotestate.WorkActivityOptions) (*remotestate.WorkActivity, error)
	CreateInitiative(ctx context.Context, req remotestate.CreateWorkInitiativeRequest) (*remotestate.WorkMutationResponse, error)
	UpsertMilestones(ctx context.Context, epicKey string, req remotestate.WorkMilestoneRequest) (*remotestate.WorkMutationResponse, error)
	// orun-initiatives-v2 (IS4) — the pen. Reads: the universal resolver's
	// context bundle, the live board, the update feed. Writes: the stored
	// initiative speech acts, the agent's voice, the generalized assign, and
	// the v4 decisions (review/verdict/approve/revoke/adopt/supersede) —
	// FINALLY nameable as tools; the human-only actor rules stay in the
	// cloud's model layer and come back as typed verdicts, and the tier
	// matrix (agents/*.md allow/ask/deny) decides who may even ask.
	GetWorkItem(ctx context.Context, ref string) (*remotestate.WorkItemResolve, error)
	GetWorkContext(ctx context.Context, ref string, opts remotestate.WorkContextOptions) (*remotestate.WorkContext, error)
	GetWorkNow(ctx context.Context, opts remotestate.WorkNowOptions) (*remotestate.WorkNow, error)
	ListInitiativeUpdates(ctx context.Context, key string) (*remotestate.WorkInitiativeUpdates, error)
	SetInitiativeStatus(ctx context.Context, key string, req remotestate.SetInitiativeStatusRequest) (*remotestate.SetInitiativeStatusResponse, error)
	PostInitiativeUpdate(ctx context.Context, key string, req remotestate.PostInitiativeUpdateRequest) (*remotestate.PostInitiativeUpdateResponse, error)
	AssertTaskDone(ctx context.Context, key, note, clientToken string) (*remotestate.WorkMutationResponse, error)
	PostTaskNote(ctx context.Context, key string, req remotestate.PostTaskNoteRequest) (*remotestate.WorkMutationResponse, error)
	AssignWorkItem(ctx context.Context, key string, req remotestate.AssignWorkItemRequest) (*remotestate.WorkMutationResponse, error)
	RequestWorkReview(ctx context.Context, collection, key string, req remotestate.WorkReviewRequest) (*remotestate.WorkMutationResponse, error)
	SubmitWorkVerdict(ctx context.Context, collection, key string, req remotestate.WorkVerdictRequest) (*remotestate.WorkMutationResponse, error)
	ApproveEpic(ctx context.Context, key string, req remotestate.ApproveEpicRequest) (*remotestate.ApproveEpicResponse, error)
	RevokeEpicApproval(ctx context.Context, key, note string) (*remotestate.WorkMutationResponse, error)
	AdoptDesign(ctx context.Context, key string, req remotestate.AdoptDesignRequest) (*remotestate.AdoptDesignResponse, error)
	SupersedeDesign(ctx context.Context, key string, req remotestate.SupersedeDesignRequest) (*remotestate.WorkMutationResponse, error)
}

// Server is the work-plane mcpserve.ToolProvider for one workspace-scoped
// client.
type Server struct {
	API       WorkAPI
	Workspace string
}

func obj(props map[string]interface{}, required ...string) map[string]interface{} {
	s := map[string]interface{}{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func str(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": desc}
}

// ToolNames returns the closed tool surface's names, in definition order —
// the list the agent runtime's MCP config writer filters through tool policy
// (internal/agent/mcp.go).
func ToolNames() []string {
	defs := Tools()
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	return names
}

// ReadOnly reports whether name is one of the surface's read tools —
// display metadata for `orun mcp tools`, derived from the wire annotations
// (UM4) so the display can never disagree with what a client sees.
func ReadOnly(name string) bool {
	for _, t := range Tools() {
		if t.Name == name {
			ro, _ := t.Annotations["readOnlyHint"].(bool)
			return ro
		}
	}
	return false
}

// Tools returns the closed tool surface. Note what is absent: no
// task-rung write (no lifecycle write exists anywhere), no pin. Deferred,
// tracked in the epic's IMPLEMENTATION-STATUS: work_yours rides the IS2b
// server leg; pr_open (the provenance pen) rides IS6 — the roster reaches
// its full 37 there.
//
// Wire annotations (orun-mcp UM4). The reads are the plain truth:
// readOnly/non-destructive/idempotent. The write tools are readOnly:false,
// destructive:false (mutator-shaped per WP-6 — every write appends or
// applies through the one mutator surface; nothing on this plane deletes
// or irreversibly overwrites), and idempotent:FALSE: every CALL appends a
// new event to the coordination log — a blind re-invocation of
// task_create/task_comment/design_propose/initiative_create duplicates the
// artifact, and task_regenerate mints fresh task keys per run. The IS-L
// writes (initiative_status_set, initiative_update_post, task_done,
// task_note, item_assign) carry `clientToken` and the client defaults it
// on, which makes the TRANSPORT retry replay-safe — but a fresh tool call
// still mints a fresh token, so the hint stays honestly false. A strict
// client should confirm before replaying a work write.
func Tools() []mcpserve.ToolDef {
	readAnn := mcpserve.Annotations(true, false, true)
	writeAnn := mcpserve.Annotations(false, false, false)
	contractSchema := obj(map[string]interface{}{
		"goal":     str("one or two sentences; the brief's first line"),
		"affects":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "catalog component keys"},
		"doneWhen": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		"gates":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "checks verified from orun execution truth"},
		"deps":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
	})
	return []mcpserve.ToolDef{
		{Name: "work_query", Description: "The workspace lens: specs with progress, tasks with DERIVED lifecycle and its evidence, the drift inbox, claim suggestions. Nothing returned is a stored status.", InputSchema: obj(map[string]interface{}{}), Annotations: readAnn},
		{Name: "work_get", Description: "One task: envelope, contract, and the fold's lifecycle with evidence.", InputSchema: obj(map[string]interface{}{"key": str("task key, e.g. ORN-142")}, "key"), Annotations: readAnn},
		{Name: "spec_get", Description: "The frozen brief: a content-addressed SpecSnapshot (intent only — contracts and docs, never a rung or assignee). Implement against exactly this.", InputSchema: obj(map[string]interface{}{"spec": str("spec slug")}, "spec"), Annotations: readAnn},
		{Name: "work_timeline", Description: "The unified timeline for one item: both logs (what people said, what the world did) interleaved by time — evidence attached, read-only.", InputSchema: obj(map[string]interface{}{"key": str("task or spec key")}, "key"), Annotations: readAnn},
		{Name: "spec_doc", Description: "A spec's cloud document revision (content-addressed, V3-2; latest when rev is omitted) — read-only.", InputSchema: obj(map[string]interface{}{"spec": str("spec slug"), "rev": str("revision digest sha256:<hex> (optional)")}, "spec"), Annotations: readAnn},
		{Name: "task_create", Description: "Create a task (e.g. discovered follow-up work) through the one mutator surface.", InputSchema: obj(map[string]interface{}{"prefix": str("task-key prefix, 2–5 uppercase"), "title": str("task title"), "spec": str("parent spec slug (optional)"), "milestone": str("milestone key within spec (optional, v4)"), "contract": contractSchema}, "prefix", "title"), Annotations: writeAnn},
		{Name: "task_comment", Description: "Append a comment to a task's coordination log.", InputSchema: obj(map[string]interface{}{"key": str("task key"), "body": str("comment body")}, "key", "body"), Annotations: writeAnn},
		{Name: "task_assign", Description: "Assign a membership subject (self-assignment claims work).", InputSchema: obj(map[string]interface{}{"key": str("task key"), "subject": str("membership subject id (usr_/sp_/team_)")}, "key", "subject"), Annotations: writeAnn},
		{Name: "contract_propose", Description: "Propose a contract change: applied through the mutators AND flagged with a review comment — an agent cannot quietly redefine its own definition of done.", InputSchema: obj(map[string]interface{}{"key": str("task key"), "contract": contractSchema}, "key", "contract"), Annotations: writeAnn},
		// v4 (WH5) — the hierarchy surface. Note what is STILL absent: no
		// approve tool, no adopt tool (human-only decisions, V4-2), and
		// still no status or pin.
		{Name: "epic_brief", Description: "The frozen brief an APPROVAL sealed: EpicSnapshot canonical bytes + content id (doc ref, milestone ladder + hash, task contracts, approval record). Implement against exactly this; verify sha256(bytes) == id. An unapproved epic has no brief.", InputSchema: obj(map[string]interface{}{"epic": str("epic slug"), "id": str("pinned snapshot id sha256:<hex> (optional; latest otherwise)")}, "epic"), Annotations: readAnn},
		{Name: "milestone_get", Description: "One epic's milestone ladder: authored goals/done-when plus DERIVED progress per milestone — read-only.", InputSchema: obj(map[string]interface{}{"epic": str("epic slug")}, "epic"), Annotations: readAnn},
		{Name: "design_get", Description: "One design: doc pointer, sealed context (what it assumed), structured proposal, and folded intent state — read-only.", InputSchema: obj(map[string]interface{}{"key": str("design key, e.g. DSG-1")}, "key"), Annotations: readAnn},
		{Name: "initiative_get", Description: "One initiative's DERIVED rollup: health with named evidence, progress, per-epic intent + execution. Nothing returned is enterable.", InputSchema: obj(map[string]interface{}{"initiative": str("initiative key")}, "initiative"), Annotations: readAnn},
		{Name: "design_propose", Description: "Create a Draft design under an initiative: a document reference plus a structured proposal (epics → milestones → task skeletons). A design is a PROPOSAL — humans review, compare, and adopt; adoption mints epics and is not available here.", InputSchema: obj(map[string]interface{}{"initiative": str("initiative key"), "title": str("design title"), "docRef": str("design doc revision sha256:<hex> (optional)"), "proposal": map[string]interface{}{"type": "object", "description": "{epics: [{slug, title, docSeed?, milestones[], taskSkeletons[]}]}"}}, "initiative", "title"), Annotations: writeAnn},
		{Name: "task_regenerate", Description: "Re-plan one milestone in one verdict batch: PLANNED (draft/ready) tasks cancel, in-flight tasks survive, and every proposed contract is applied AND flagged for human review. Tasks are implementation detail (V4-5) — this never touches the epic's approval.", InputSchema: obj(map[string]interface{}{"epic": str("epic slug"), "milestone": str("milestone key, e.g. M1"), "prefix": str("task-key prefix (default WK)"), "tasks": map[string]interface{}{"type": "array", "items": obj(map[string]interface{}{"title": str("task title"), "contract": contractSchema}, "title"), "description": "the replacement plan"}}, "epic", "milestone", "tasks"), Annotations: writeAnn},
		// orun-initiatives (IN5) — the portfolio surface: four derived
		// reads and two envelope writes. STILL absent, on purpose: no
		// approve, no adopt, no supersede, no pin — those are human-only
		// decisions with no tool to name them; a write that brushes one
		// gets the cloud's typed human_only verdict, surfaced verbatim.
		{Name: "initiatives_list", Description: "The portfolio in one read: every initiative with DERIVED status (planning until the first approved epic, the health fold afterwards), two-segment progress (done/active/total), the needs-you reasons that wait on a human, agent assignees, epic rows with intent state, design rows, plus workspace fold-stats. Nothing returned is a stored status.", InputSchema: obj(map[string]interface{}{}), Annotations: readAnn},
		{Name: "initiative_tree", Description: "One initiative's full hierarchy — the world to plan against: epics with intent state and approved revision (drift named), milestones with derived state (complete/active/upcoming) and progress, tasks with rungs and delivery evidence, docs with revisions and open threads, and the initiative's design runs.", InputSchema: obj(map[string]interface{}{"key": str("initiative key")}, "key"), Annotations: readAnn},
		{Name: "task_get", Description: "One task's whole page: the task view (derived rung with evidence, contract, pins), its ancestry (initiative/epic/milestone), the folded delivery evidence (branch, PR, checks), components affected (observation diffstats — empty when the world reported none, never invented), and the task-scoped activity tail, newest first.", InputSchema: obj(map[string]interface{}{"key": str("task key, e.g. ORN-142")}, "key"), Annotations: readAnn},
		{Name: "activity_get", Description: "The tagged activity tail for any noun: both logs folded into one reverse-chronological list of neutral server sentences. The tag trail is ancestry — filtering by an epic covers its milestones' tasks, docs, and designs; an initiative covers its whole subtree. Omit tag for the workspace-wide tail; page with cursor.", InputSchema: obj(map[string]interface{}{"tag": str("item key to filter by, ancestry included (optional)"), "limit": map[string]interface{}{"type": "integer", "description": "maximum entries to return (optional)"}, "cursor": str("resume cursor from a prior page (optional)")}), Annotations: readAnn},
		{Name: "initiative_create", Description: "Create an initiative envelope (slug + title, optional description/owner/targetDate/successCriteria) through the one mutator surface. Agents may draft the envelope; the why (success criteria) stays human-edited. An initiative has no lifecycle — status derives from its member epics' logs.", InputSchema: obj(map[string]interface{}{"slug": str("initiative slug, lowercase kebab"), "title": str("initiative title"), "description": str("initiative description (optional)"), "owner": str("owner subject (optional)"), "targetDate": str("target date YYYY-MM-DD (optional)"), "successCriteria": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "success criteria bullets (optional)"}}, "slug", "title"), Annotations: writeAnn},
		{Name: "milestone_upsert", Description: "Apply one ladder edit to an epic's milestones: op create/edit/reorder/remove on one milestone key. Authored intent only (title, goal, doneWhen, targetDate, ordinal) — per-milestone progress stays derived and cannot be entered. Editing an approved epic's ladder drifts the approval (ladderHash); a human re-approves.", InputSchema: obj(map[string]interface{}{"epic": str("epic slug"), "op": str("create | edit | reorder | remove"), "key": str("milestone key, e.g. M2"), "title": str("milestone title (create/edit)"), "goal": str("milestone goal (optional)"), "doneWhen": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "done-when criteria (optional)"}, "targetDate": str("target date YYYY-MM-DD (optional)"), "ordinal": map[string]interface{}{"type": "integer", "description": "ladder position (reorder)"}}, "epic", "op", "key"), Annotations: writeAnn},
		// orun-initiatives-v2 (IS4) — the pen. Three reads and eleven
		// writes; tier defaults per agent type live in agents/*.md (the
		// signature-shaped tools ride the ask lane, where the human
		// confirmation IS the signature; an unattended session auto-denies).
		// Deferred: work_yours (IS2b), pr_open (IS6).
		{Name: "work_context", Description: "The any-key context bundle — the intended FIRST call of every session: give any key (task, milestone, epic, design, initiative; typed, letterless, alias, or machine id) and get the item's full view, its ancestry to the root with live states, the activity tail, and the open needs-you reasons in scope. Truncation is always echoed in budget[] with cursors — no silent caps.", InputSchema: obj(map[string]interface{}{"key": str("any item key or ref, e.g. PAY-T14, PAY-E2#M1, tsk_…, or a slug"), "depth": map[string]interface{}{"type": "integer", "description": "subtree depth below the item (default 2, max 4)"}, "perLevel": map[string]interface{}{"type": "integer", "description": "children per level (default 50, max 200)"}, "activity": map[string]interface{}{"type": "integer", "description": "activity tail length (default 20, max 100)"}}, "key"), Annotations: readAnn},
		{Name: "work_now", Description: "The live board: every in-flight task × its latest worklog note × the seat working it, quiet chips derived at read. The \"what is every agent doing right now\" read; filter by initiative, epic, or seat; cursor-paged.", InputSchema: obj(map[string]interface{}{"initiative": str("filter to one initiative key (optional)"), "epic": str("filter to one epic key (optional)"), "seat": str("filter to one seat id, e.g. sp_… (optional)"), "limit": map[string]interface{}{"type": "integer", "description": "maximum rows (optional)"}, "cursor": str("resume cursor from a prior page (optional)")}), Annotations: readAnn},
		{Name: "initiative_updates_get", Description: "One initiative's update feed, newest first: the attributed health headlines humans and owner-agents posted. Health is never a formula here — it is the latest update's word (staleness derives at read).", InputSchema: obj(map[string]interface{}{"key": str("initiative key")}, "key"), Annotations: readAnn},
		{Name: "item_assign", Description: "Assign a membership subject to ANY noun — a task (claims work), a design (names the author), an epic or initiative (names the owner). Absorbs task_assign, which stays registered. The dispatch gate is unchanged: sp_ into a non-approved epic still refuses unless a human supplies the attributed override note.", InputSchema: obj(map[string]interface{}{"key": str("item key (task, design, epic, initiative)"), "subject": str("membership subject id (usr_/sp_/team_)"), "unassign": map[string]interface{}{"type": "boolean", "description": "remove instead of add (optional)"}, "override": str("attributed override note for the dispatch gate (optional; human-supplied)"), "clientToken": str("idempotency token (optional; defaulted on)")}, "key", "subject"), Annotations: writeAnn},
		{Name: "review_request", Description: "Request review on an epic or a design: appends review_requested and surfaces the item in reviewers' queues. An agent asking for eyes is the lifecycle working as designed.", InputSchema: obj(map[string]interface{}{"key": str("epic slug or design key"), "note": str("what to look at (optional)"), "revision": str("doc revision sha256:<hex> under review (optional)"), "reviewers": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "reviewer subjects (optional)"}}, "key"), Annotations: writeAnn},
		{Name: "review_verdict", Description: "Submit a review verdict: approve | request_changes, with the reasoning note (mandatory — an opinion without a reason is a vote, not a review). A verdict is an OPINION; the decision (epic approval, design adoption) stays a separate, human-signed act.", InputSchema: obj(map[string]interface{}{"key": str("epic slug or design key"), "verdict": str("approve | request_changes"), "note": str("the reasoning (required)"), "revision": str("doc revision the verdict pins (optional)")}, "key", "verdict", "note"), Annotations: writeAnn},
		{Name: "task_done", Description: "Assert a task is done, with the mandatory note saying why (the assertion lane, design §7.1). The WEAKEST voice: live delivery evidence at in_review or above wins over the assertion, released stays evidence-only, and the lifecycle names who asserted. Use when the work is finished but the world's evidence hasn't landed (or never will — research, ops, docs).", InputSchema: obj(map[string]interface{}{"key": str("task key"), "note": str("why the work is done (required)"), "clientToken": str("idempotency token (optional; defaulted on)")}, "key", "note"), Annotations: writeAnn},
		{Name: "task_note", Description: "Append a worklog note — the live \"now\" line on the task (≤280 chars; 1/min per task per seat, daily cap; beyond the clamp comes a typed rate_limited verdict). Narration is INERT: it moves no rung, feeds no health, triggers nothing (IS-8). Post one when starting, at meaningful turns, and when handing off.", InputSchema: obj(map[string]interface{}{"key": str("task key"), "text": str("the now line, e.g. \"tests green, writing the migration\""), "ref": str("evidence anchor: commit sha, file path, PR number (optional)"), "clientToken": str("idempotency token (optional; defaulted on)")}, "key", "text"), Annotations: writeAnn},
		{Name: "initiative_update_post", Description: "Post an attributed initiative update: a health word (on_track | at_risk | off_track) plus the narrative body. This is how health EXISTS — it is the latest update's headline, never a formula; the derived signals only suggest. Owner-agents post these on cadence.", InputSchema: obj(map[string]interface{}{"key": str("initiative key"), "health": str("on_track | at_risk | off_track"), "body": str("the update narrative"), "clientToken": str("idempotency token (optional; defaulted on)")}, "key", "health", "body"), Annotations: writeAnn},
		{Name: "initiative_status_set", Description: "Move an initiative through the stored five-state machine (planning → active ⇄ paused → completed; cancel/reopen/restore). start/pause/resume are agent-legal; complete/cancel/reopen/restore are SIGNATURES — human-only server-side (typed human_only verdict for agent seats), ask-tier in the agent policy so a human confirmation carries them. An illegal move answers 409 naming the allowed transitions.", InputSchema: obj(map[string]interface{}{"key": str("initiative key"), "to": str("planning | active | paused | completed | canceled"), "comment": str("why (optional; recorded on the transition)"), "force": map[string]interface{}{"type": "boolean", "description": "acknowledge open member tasks on complete (optional)"}, "clientToken": str("idempotency token (optional; defaulted on)")}, "key", "to"), Annotations: writeAnn},
		{Name: "design_adopt", Description: "Adopt a design: mints the proposed structure (epics → milestones → task skeletons) AND approves the minted epics at rev 0 in one transaction — one signature covers what it mints. Human-only server-side; ask-tier in every agent policy (the confirmation is the signature; an sp_ seat's ask auto-denies).", InputSchema: obj(map[string]interface{}{"key": str("design key"), "epics": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "subset of proposal epic slugs to mint (optional; default all)"}, "taskPrefix": str("task-key prefix for minted skeletons (optional)")}, "key"), Annotations: writeAnn},
		{Name: "design_supersede", Description: "Supersede a design — retire it, optionally naming the design that replaces it. Human-only server-side; ask-tier in every agent policy.", InputSchema: obj(map[string]interface{}{"key": str("design key to supersede"), "by": str("the replacing design key (optional)"), "note": str("why (optional)")}, "key"), Annotations: writeAnn},
		{Name: "epic_approve", Description: "Approve an epic at a revision — seals the EpicSnapshot brief (the frozen dispatch artifact) and opens the dispatch gate. Re-approval after drift pins the new revision. Human-only server-side; ask-tier in every agent policy (the confirmation is the signature).", InputSchema: obj(map[string]interface{}{"key": str("epic slug"), "revision": str("doc revision sha256:<hex> to approve"), "minApprovals": map[string]interface{}{"type": "integer", "description": "required approving verdicts (optional)"}}, "key", "revision"), Annotations: writeAnn},
		{Name: "epic_revoke_approval", Description: "Revoke an epic's approval with the mandatory note saying why — closes the dispatch gate. Human-only server-side; ask-tier in every agent policy.", InputSchema: obj(map[string]interface{}{"key": str("epic slug"), "note": str("why the approval is withdrawn (required)")}, "key", "note"), Annotations: writeAnn},
	}
}

// Tools implements mcpserve.ToolProvider.
func (s *Server) Tools() []mcpserve.ToolDef { return Tools() }

// Call implements mcpserve.ToolProvider: owned=false for names outside the
// work roster (another provider's business), and every owned failure maps
// to an isError result — the mutator's verdict is something the agent
// should reason about, not a protocol fault.
func (s *Server) Call(ctx context.Context, name string, args json.RawMessage) (mcpserve.Result, bool) {
	if !toolNames[name] {
		return nil, false
	}
	result, err := s.call(ctx, name, args)
	if err != nil {
		return toolText(fmt.Sprintf("error: %v", err), true), true
	}
	return result, true
}

// toolNames is the owned roster, derived from Tools() so ownership can
// never drift from the advertised surface.
var toolNames = func() map[string]bool {
	m := map[string]bool{}
	for _, t := range Tools() {
		m[t.Name] = true
	}
	return m
}()

func toolText(text string, isErr bool) mcpserve.Result {
	return mcpserve.TextResult(text, isErr)
}

func toolJSON(v interface{}) (mcpserve.Result, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return toolText(string(b), false), nil
}

func (s *Server) call(ctx context.Context, name string, args json.RawMessage) (mcpserve.Result, error) {
	switch name {
	case "work_query":
		summary, err := s.API.GetWorkSummary(ctx)
		if err != nil {
			return nil, err
		}
		return toolJSON(summary)

	case "work_get":
		var a struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" {
			return nil, fmt.Errorf("work_get: key is required")
		}
		summary, err := s.API.GetWorkSummary(ctx)
		if err != nil {
			return nil, err
		}
		for _, t := range summary.Tasks {
			if t.Key == a.Key {
				return toolJSON(t)
			}
		}
		return nil, fmt.Errorf("work_get: unknown task %s", a.Key)

	case "spec_get":
		var a struct {
			Spec string `json:"spec"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Spec == "" {
			return nil, fmt.Errorf("spec_get: spec is required")
		}
		summary, err := s.API.GetWorkSummary(ctx)
		if err != nil {
			return nil, err
		}
		snap, err := workbrief.SnapshotFromSummary(s.Workspace, a.Spec, summary)
		if err != nil {
			return nil, err
		}
		id, canonical, err := worklens.SealSpecSnapshot(*snap)
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("%s\n%s", id, canonical), false), nil

	case "work_timeline":
		var a struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" {
			return nil, fmt.Errorf("work_timeline: key is required")
		}
		timeline, err := s.API.GetWorkTimeline(ctx, a.Key)
		if err != nil {
			return nil, err
		}
		return toolJSON(timeline)

	case "spec_doc":
		var a struct {
			Spec string `json:"spec"`
			Rev  string `json:"rev"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Spec == "" {
			return nil, fmt.Errorf("spec_doc: spec is required")
		}
		doc, err := s.API.GetWorkDoc(ctx, a.Spec, a.Rev)
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("%s (parent %s)\n\n%s", doc.Revision, doc.Parent, doc.Body), false), nil

	case "task_create":
		var a struct {
			Prefix    string                    `json:"prefix"`
			Title     string                    `json:"title"`
			Spec      string                    `json:"spec"`
			Milestone string                    `json:"milestone"`
			Contract  *remotestate.WorkContract `json:"contract"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Prefix == "" || a.Title == "" {
			return nil, fmt.Errorf("task_create: prefix and title are required")
		}
		out, err := s.API.CreateWorkTask(ctx, remotestate.CreateWorkTaskRequest{
			Prefix: a.Prefix, Title: a.Title, SpecKey: a.Spec, Milestone: a.Milestone, Contract: a.Contract,
		})
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("created %s (event seq %d)", out.Key, out.Seq), false), nil

	case "task_comment":
		var a struct {
			Key  string `json:"key"`
			Body string `json:"body"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" || a.Body == "" {
			return nil, fmt.Errorf("task_comment: key and body are required")
		}
		out, err := s.API.CommentWork(ctx, a.Key, a.Body)
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("commented on %s (event seq %d)", out.Key, out.Seq), false), nil

	case "task_assign":
		// Absorbed by item_assign (IS4): the name stays registered forever
		// (no tool renamed, ever), forwarding to the generalized assign.
		var a struct {
			Key     string `json:"key"`
			Subject string `json:"subject"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" || a.Subject == "" {
			return nil, fmt.Errorf("task_assign: key and subject are required")
		}
		out, err := s.API.AssignWorkItem(ctx, a.Key, remotestate.AssignWorkItemRequest{Subject: a.Subject})
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("assigned %s to %s (event seq %d)", out.Key, a.Subject, out.Seq), false), nil

	case "contract_propose":
		var a struct {
			Key      string                   `json:"key"`
			Contract remotestate.WorkContract `json:"contract"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" {
			return nil, fmt.Errorf("contract_propose: key and contract are required")
		}
		out, err := s.API.EditWorkContract(ctx, a.Key, a.Contract)
		if err != nil {
			return nil, err
		}
		// The flag: a proposal is applied AND surfaced for human review —
		// an agent cannot quietly redefine its own definition of done.
		if _, err := s.API.CommentWork(ctx, a.Key, "contract proposed via MCP — human review requested"); err != nil {
			return nil, fmt.Errorf("contract applied (seq %d) but review flag failed: %w", out.Seq, err)
		}
		return toolText(fmt.Sprintf("contract proposed on %s (event seq %d); flagged for human review", out.Key, out.Seq), false), nil

	case "epic_brief":
		var a struct {
			Epic string `json:"epic"`
			ID   string `json:"id"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Epic == "" {
			return nil, fmt.Errorf("epic_brief: epic is required")
		}
		brief, err := s.API.GetEpicBrief(ctx, a.Epic, a.ID)
		if err != nil {
			return nil, err
		}
		if err := worklens.VerifySealedBytes(brief.ID, []byte(brief.Canonical)); err != nil {
			return nil, fmt.Errorf("epic_brief: %w", err)
		}
		return toolText(fmt.Sprintf("%s\n%s", brief.ID, brief.Canonical), false), nil

	case "milestone_get":
		var a struct {
			Epic string `json:"epic"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Epic == "" {
			return nil, fmt.Errorf("milestone_get: epic is required")
		}
		view, err := s.API.GetEpicMilestones(ctx, a.Epic)
		if err != nil {
			return nil, err
		}
		return toolJSON(view)

	case "design_get":
		var a struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" {
			return nil, fmt.Errorf("design_get: key is required")
		}
		design, err := s.API.GetWorkDesign(ctx, a.Key)
		if err != nil {
			return nil, err
		}
		return toolJSON(design)

	case "initiative_get":
		var a struct {
			Initiative string `json:"initiative"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Initiative == "" {
			return nil, fmt.Errorf("initiative_get: initiative is required")
		}
		rollups, err := s.API.GetWorkRollups(ctx, a.Initiative)
		if err != nil {
			return nil, err
		}
		return toolJSON(rollups)

	case "design_propose":
		var a struct {
			Initiative string          `json:"initiative"`
			Title      string          `json:"title"`
			DocRef     string          `json:"docRef"`
			Proposal   json.RawMessage `json:"proposal"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Initiative == "" || a.Title == "" {
			return nil, fmt.Errorf("design_propose: initiative and title are required")
		}
		out, err := s.API.CreateWorkDesign(ctx, a.Initiative, remotestate.CreateWorkDesignRequest{
			Title: a.Title, DocRef: a.DocRef, Proposal: a.Proposal,
		})
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("proposed design %s (event seq %d) — a human reviews, compares, and adopts; adoption mints the epics", out.Key, out.Seq), false), nil

	case "task_regenerate":
		var a struct {
			Epic      string `json:"epic"`
			Milestone string `json:"milestone"`
			Prefix    string `json:"prefix"`
			Tasks     []struct {
				Title    string                    `json:"title"`
				Contract *remotestate.WorkContract `json:"contract"`
			} `json:"tasks"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Epic == "" || a.Milestone == "" || len(a.Tasks) == 0 {
			return nil, fmt.Errorf("task_regenerate: epic, milestone, and tasks are required")
		}
		req := remotestate.RegenerateWorkTasksRequest{Prefix: a.Prefix}
		for _, t := range a.Tasks {
			req.Tasks = append(req.Tasks, struct {
				Title    string                    `json:"title"`
				Contract *remotestate.WorkContract `json:"contract,omitempty"`
			}{Title: t.Title, Contract: t.Contract})
		}
		out, err := s.API.RegenerateWorkTasks(ctx, a.Epic, a.Milestone, req)
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("regenerated %s/%s: created %v, canceled %v, kept in-flight %v — proposed contracts are flagged for human review", a.Epic, a.Milestone, out.Created, out.Canceled, out.Kept), false), nil

	case "initiatives_list":
		portfolio, err := s.API.ListInitiatives(ctx)
		if err != nil {
			return nil, err
		}
		return toolJSON(portfolio)

	case "initiative_tree":
		var a struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" {
			return nil, fmt.Errorf("initiative_tree: key is required")
		}
		tree, err := s.API.GetInitiativeTree(ctx, a.Key)
		if err != nil {
			return nil, err
		}
		return toolJSON(tree)

	case "task_get":
		var a struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" {
			return nil, fmt.Errorf("task_get: key is required")
		}
		detail, err := s.API.GetTaskDetail(ctx, a.Key)
		if err != nil {
			return nil, err
		}
		return toolJSON(detail)

	case "activity_get":
		var a struct {
			Tag    string `json:"tag"`
			Limit  int    `json:"limit"`
			Cursor string `json:"cursor"`
		}
		if len(args) > 0 {
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, fmt.Errorf("activity_get: invalid arguments")
			}
		}
		activity, err := s.API.GetWorkActivity(ctx, remotestate.WorkActivityOptions{
			Tag: a.Tag, Limit: a.Limit, Cursor: a.Cursor,
		})
		if err != nil {
			return nil, err
		}
		return toolJSON(activity)

	case "initiative_create":
		var a struct {
			Slug            string   `json:"slug"`
			Title           string   `json:"title"`
			Description     string   `json:"description"`
			Owner           string   `json:"owner"`
			TargetDate      string   `json:"targetDate"`
			SuccessCriteria []string `json:"successCriteria"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Slug == "" || a.Title == "" {
			return nil, fmt.Errorf("initiative_create: slug and title are required")
		}
		out, err := s.API.CreateInitiative(ctx, remotestate.CreateWorkInitiativeRequest{
			Slug: a.Slug, Title: a.Title, Description: a.Description,
			Owner: a.Owner, TargetDate: a.TargetDate, SuccessCriteria: a.SuccessCriteria,
		})
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("created initiative %s (event seq %d)", out.Key, out.Seq), false), nil

	case "milestone_upsert":
		var a struct {
			Epic       string   `json:"epic"`
			Op         string   `json:"op"`
			Key        string   `json:"key"`
			Title      string   `json:"title"`
			Goal       string   `json:"goal"`
			DoneWhen   []string `json:"doneWhen"`
			TargetDate string   `json:"targetDate"`
			Ordinal    *int     `json:"ordinal"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Epic == "" || a.Op == "" || a.Key == "" {
			return nil, fmt.Errorf("milestone_upsert: epic, op, and key are required")
		}
		out, err := s.API.UpsertMilestones(ctx, a.Epic, remotestate.WorkMilestoneRequest{
			Op: a.Op, Key: a.Key, Title: a.Title, Goal: a.Goal,
			DoneWhen: a.DoneWhen, TargetDate: a.TargetDate, Ordinal: a.Ordinal,
		})
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("applied %s to milestone %s on %s (event seq %d)", a.Op, a.Key, a.Epic, out.Seq), false), nil

	// ── orun-initiatives-v2 (IS4) — the pen ─────────────────────────────────

	case "work_context":
		var a struct {
			Key      string `json:"key"`
			Depth    int    `json:"depth"`
			PerLevel int    `json:"perLevel"`
			Activity int    `json:"activity"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" {
			return nil, fmt.Errorf("work_context: key is required")
		}
		bundle, err := s.API.GetWorkContext(ctx, a.Key, remotestate.WorkContextOptions{
			Depth: a.Depth, PerLevel: a.PerLevel, Activity: a.Activity,
		})
		if err != nil {
			return nil, err
		}
		return toolJSON(bundle)

	case "work_now":
		var a struct {
			Initiative string `json:"initiative"`
			Epic       string `json:"epic"`
			Seat       string `json:"seat"`
			Limit      int    `json:"limit"`
			Cursor     string `json:"cursor"`
		}
		if len(args) > 0 {
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, fmt.Errorf("work_now: invalid arguments")
			}
		}
		board, err := s.API.GetWorkNow(ctx, remotestate.WorkNowOptions{
			Initiative: a.Initiative, Epic: a.Epic, Seat: a.Seat, Limit: a.Limit, Cursor: a.Cursor,
		})
		if err != nil {
			return nil, err
		}
		return toolJSON(board)

	case "initiative_updates_get":
		var a struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" {
			return nil, fmt.Errorf("initiative_updates_get: key is required")
		}
		updates, err := s.API.ListInitiativeUpdates(ctx, a.Key)
		if err != nil {
			return nil, err
		}
		return toolJSON(updates)

	case "item_assign":
		var a struct {
			Key         string `json:"key"`
			Subject     string `json:"subject"`
			Unassign    bool   `json:"unassign"`
			Override    string `json:"override"`
			ClientToken string `json:"clientToken"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" || a.Subject == "" {
			return nil, fmt.Errorf("item_assign: key and subject are required")
		}
		out, err := s.API.AssignWorkItem(ctx, a.Key, remotestate.AssignWorkItemRequest{
			Subject: a.Subject, Unassign: a.Unassign, Override: a.Override, ClientToken: a.ClientToken,
		})
		if err != nil {
			return nil, err
		}
		verb := "assigned"
		if a.Unassign {
			verb = "unassigned"
		}
		return toolText(fmt.Sprintf("%s %s ↔ %s (event seq %d)", verb, out.Key, a.Subject, out.Seq), false), nil

	case "review_request":
		var a struct {
			Key       string   `json:"key"`
			Note      string   `json:"note"`
			Revision  string   `json:"revision"`
			Reviewers []string `json:"reviewers"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" {
			return nil, fmt.Errorf("review_request: key is required")
		}
		out, err := s.API.RequestWorkReview(ctx, remotestate.ReviewCollectionOf(a.Key), a.Key, remotestate.WorkReviewRequest{
			Revision: a.Revision, Reviewers: a.Reviewers, Note: a.Note,
		})
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("review requested on %s (event seq %d)", out.Key, out.Seq), false), nil

	case "review_verdict":
		var a struct {
			Key      string `json:"key"`
			Verdict  string `json:"verdict"`
			Note     string `json:"note"`
			Revision string `json:"revision"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" || a.Verdict == "" || a.Note == "" {
			return nil, fmt.Errorf("review_verdict: key, verdict, and note are required — an opinion without a reason is a vote, not a review")
		}
		out, err := s.API.SubmitWorkVerdict(ctx, remotestate.ReviewCollectionOf(a.Key), a.Key, remotestate.WorkVerdictRequest{
			Revision: a.Revision, Verdict: a.Verdict, Note: a.Note,
		})
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("verdict %s recorded on %s (event seq %d) — an opinion, not a decision", a.Verdict, out.Key, out.Seq), false), nil

	case "task_done":
		var a struct {
			Key         string `json:"key"`
			Note        string `json:"note"`
			ClientToken string `json:"clientToken"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" || a.Note == "" {
			return nil, fmt.Errorf("task_done: key and note are required — say why the work is done")
		}
		out, err := s.API.AssertTaskDone(ctx, a.Key, a.Note, a.ClientToken)
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("done asserted on %s (event seq %d) — the weakest voice: live evidence at in_review+ wins", out.Key, out.Seq), false), nil

	case "task_note":
		var a struct {
			Key         string `json:"key"`
			Text        string `json:"text"`
			Ref         string `json:"ref"`
			ClientToken string `json:"clientToken"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" || a.Text == "" {
			return nil, fmt.Errorf("task_note: key and text are required")
		}
		out, err := s.API.PostTaskNote(ctx, a.Key, remotestate.PostTaskNoteRequest{
			Text: a.Text, Ref: a.Ref, ClientToken: a.ClientToken,
		})
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("noted on %s (event seq %d) — narration is inert; it moves nothing", out.Key, out.Seq), false), nil

	case "initiative_update_post":
		var a struct {
			Key         string `json:"key"`
			Health      string `json:"health"`
			Body        string `json:"body"`
			ClientToken string `json:"clientToken"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" || a.Health == "" || a.Body == "" {
			return nil, fmt.Errorf("initiative_update_post: key, health, and body are required")
		}
		out, err := s.API.PostInitiativeUpdate(ctx, a.Key, remotestate.PostInitiativeUpdateRequest{
			Health: a.Health, Body: a.Body, ClientToken: a.ClientToken,
		})
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("update posted on %s (event seq %d) — health headline now %s", out.Key, out.Seq, out.Update.Health), false), nil

	case "initiative_status_set":
		var a struct {
			Key         string `json:"key"`
			To          string `json:"to"`
			Comment     string `json:"comment"`
			Force       bool   `json:"force"`
			ClientToken string `json:"clientToken"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" || a.To == "" {
			return nil, fmt.Errorf("initiative_status_set: key and to are required")
		}
		out, err := s.API.SetInitiativeStatus(ctx, a.Key, remotestate.SetInitiativeStatusRequest{
			To: a.To, Comment: a.Comment, Force: a.Force, ClientToken: a.ClientToken,
		})
		if err != nil {
			return nil, err
		}
		text := fmt.Sprintf("%s is now %s (event seq %d)", out.Key, out.Status, out.Seq)
		if out.Warning != "" {
			text += " — " + out.Warning
		}
		return toolText(text, false), nil

	case "design_adopt":
		var a struct {
			Key        string   `json:"key"`
			Epics      []string `json:"epics"`
			TaskPrefix string   `json:"taskPrefix"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" {
			return nil, fmt.Errorf("design_adopt: key is required")
		}
		out, err := s.API.AdoptDesign(ctx, a.Key, remotestate.AdoptDesignRequest{
			Epics: a.Epics, TaskPrefix: a.TaskPrefix,
		})
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("adopted %s (event seq %d): minted epics %v, task skeletons %v — the minted epics are approved at rev 0 under the same signature", a.Key, out.Seq, out.Minted, out.Tasks), false), nil

	case "design_supersede":
		var a struct {
			Key  string `json:"key"`
			By   string `json:"by"`
			Note string `json:"note"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" {
			return nil, fmt.Errorf("design_supersede: key is required")
		}
		out, err := s.API.SupersedeDesign(ctx, a.Key, remotestate.SupersedeDesignRequest{By: a.By, Note: a.Note})
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("superseded %s (event seq %d)", out.Key, out.Seq), false), nil

	case "epic_approve":
		var a struct {
			Key          string `json:"key"`
			Revision     string `json:"revision"`
			MinApprovals int    `json:"minApprovals"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" || a.Revision == "" {
			return nil, fmt.Errorf("epic_approve: key and revision are required — an approval always pins the revision it read")
		}
		out, err := s.API.ApproveEpic(ctx, a.Key, remotestate.ApproveEpicRequest{
			Revision: a.Revision, MinApprovals: a.MinApprovals,
		})
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("approved %s (event seq %d) — sealed brief %s", out.Key, out.Seq, out.Snapshot), false), nil

	case "epic_revoke_approval":
		var a struct {
			Key  string `json:"key"`
			Note string `json:"note"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Key == "" || a.Note == "" {
			return nil, fmt.Errorf("epic_revoke_approval: key and note are required — say why the approval is withdrawn")
		}
		out, err := s.API.RevokeEpicApproval(ctx, a.Key, a.Note)
		if err != nil {
			return nil, err
		}
		return toolText(fmt.Sprintf("approval revoked on %s (event seq %d) — the dispatch gate is closed", out.Key, out.Seq), false), nil

	default:
		return nil, fmt.Errorf("unknown tool %s", name)
	}
}
