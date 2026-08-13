package remotestate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Work-plane client (orun-work v2 WP1) — the CLI's seam onto the cloud work
// API (/v1/organizations/{org}/work). Wire shapes mirror the platform's
// @saas/contracts/work. Deliberately small: import apply + the fold summary;
// lifecycle is derived server-side on every read and there is no
// status-writing call to offer (WP-3).

// WorkImportSpec mirrors the CLI import plan's spec entry.
type WorkImportSpec struct {
	Slug       string `json:"slug"`
	Title      string `json:"title"`
	DocPath    string `json:"docPath"`
	DocSHA256  string `json:"docSha256"`
	PlanPath   string `json:"planPath,omitempty"`
	Initiative string `json:"initiative,omitempty"`
}

// WorkImportTask mirrors the CLI import plan's task entry. No lifecycle
// field exists by design — rungs derive from observations after apply.
type WorkImportTask struct {
	SpecSlug    string        `json:"specSlug"`
	MilestoneID string        `json:"milestoneId"`
	Milestone   string        `json:"milestone,omitempty"`
	Title       string        `json:"title"`
	Contract    *WorkContract `json:"contract,omitempty"`
}

// WorkContract is the wire form of the task contract.
type WorkContract struct {
	Goal         string   `json:"goal,omitempty"`
	Affects      []string `json:"affects,omitempty"`
	DoneWhen     []string `json:"doneWhen,omitempty"`
	Gates        []string `json:"gates,omitempty"`
	DesignRefs   []string `json:"designRefs,omitempty"`
	Deps         []string `json:"deps,omitempty"`
	GatesDefined bool     `json:"gatesDefined,omitempty"`
}

// WorkImportInitiative mirrors the plan's initiative entry (v4 WH6).
type WorkImportInitiative struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// WorkImportMilestone mirrors the plan's milestone entry (v4 WH6).
type WorkImportMilestone struct {
	SpecSlug string   `json:"specSlug"`
	Key      string   `json:"key"`
	Title    string   `json:"title"`
	Goal     string   `json:"goal,omitempty"`
	DoneWhen []string `json:"doneWhen,omitempty"`
	Ordinal  int      `json:"ordinal"`
}

// WorkImportRequest is the apply body (the dry-run plan, verbatim).
type WorkImportRequest struct {
	Workspace   string                 `json:"workspace"`
	Root        string                 `json:"root"`
	Prefix      string                 `json:"prefix,omitempty"`
	Initiatives []WorkImportInitiative `json:"initiatives,omitempty"`
	Specs       []WorkImportSpec       `json:"specs"`
	Milestones  []WorkImportMilestone  `json:"milestones,omitempty"`
	Tasks       []WorkImportTask       `json:"tasks"`
}

// WorkImportResponse reports apply counts; re-imports skip idempotently.
type WorkImportResponse struct {
	SpecsCreated int `json:"specsCreated"`
	SpecsSkipped int `json:"specsSkipped"`
	TasksCreated int `json:"tasksCreated"`
	TasksSkipped int `json:"tasksSkipped"`
	// v4 (WH6) — additive; zero on pre-v4 servers.
	InitiativesCreated int `json:"initiativesCreated,omitempty"`
	InitiativesSkipped int `json:"initiativesSkipped,omitempty"`
	MilestonesCreated  int `json:"milestonesCreated,omitempty"`
	MilestonesSkipped  int `json:"milestonesSkipped,omitempty"`
	TasksMigrated      int `json:"tasksMigrated,omitempty"`
}

// WorkActor is a membership subject on the wire.
type WorkActor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Via  string `json:"via,omitempty"`
}

// WorkLifecycle is the fold's per-task output: a rung WITH its evidence.
type WorkLifecycle struct {
	Rung     string   `json:"rung"`
	Ready    bool     `json:"ready"`
	Blocked  bool     `json:"blocked"`
	Evidence []string `json:"evidence,omitempty"`
}

// WorkTaskView is one task in the summary.
type WorkTaskView struct {
	Key       string            `json:"key"`
	Spec      string            `json:"spec,omitempty"`
	Title     string            `json:"title"`
	Labels    map[string]string `json:"labels,omitempty"`
	Contract  *WorkContract     `json:"contract,omitempty"`
	CreatedBy WorkActor         `json:"createdBy"`
	CreatedAt string            `json:"createdAt,omitempty"`
	Lifecycle WorkLifecycle     `json:"lifecycle"`
}

// WorkSpecView is one spec in the summary with its derived progress.
type WorkSpecView struct {
	Key       string         `json:"key"`
	Title     string         `json:"title"`
	DocRef    string         `json:"docRef,omitempty"`
	CreatedBy WorkActor      `json:"createdBy"`
	CreatedAt string         `json:"createdAt,omitempty"`
	Progress  map[string]int `json:"progress"`
}

// WorkInitiativeView mirrors the platform's WorkInitiativeView: the
// envelope plus the DERIVED health/progress projections (v3 PM3/v4 —
// nothing here is a number anyone types).
type WorkInitiativeView struct {
	Key             string         `json:"key"`
	Title           string         `json:"title"`
	Description     string         `json:"description,omitempty"`
	Owner           string         `json:"owner,omitempty"`
	TargetDate      string         `json:"targetDate,omitempty"`
	SuccessCriteria []string       `json:"successCriteria,omitempty"`
	Health          string         `json:"health,omitempty"`
	HealthEvidence  []string       `json:"healthEvidence,omitempty"`
	CreatedBy       WorkActor      `json:"createdBy"`
	CreatedAt       string         `json:"createdAt,omitempty"`
	Specs           []string       `json:"specs,omitempty"`
	Progress        map[string]int `json:"progress,omitempty"`
}

// WorkSummary is the workspace lens: everything derives from the two logs.
type WorkSummary struct {
	Specs       []WorkSpecView       `json:"specs"`
	Tasks       []WorkTaskView       `json:"tasks"`
	Initiatives []WorkInitiativeView `json:"initiatives,omitempty"`
	CoordSeq    int64                `json:"coordSeq"`
	ObsSeq      int64                `json:"obsSeq"`
}

// workPath builds an org-scoped work path (no project segment — the work
// plane is workspace-scoped, WP-7).
func (c *Client) workPath(suffix string) string {
	return "/v1/organizations/" + urlSegment(c.scope.orgSegment()) + "/work" + suffix
}

// ImportWork applies a dry-run import plan through the cloud mutators.
// Every resulting event carries actor via=import; nothing about lifecycle
// crosses this wire.
func (c *Client) ImportWork(ctx context.Context, req WorkImportRequest) (*WorkImportResponse, error) {
	var resp WorkImportResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/import"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetWorkSummary fetches the fold summary (rungs with evidence).
func (c *Client) GetWorkSummary(ctx context.Context) (*WorkSummary, error) {
	var resp WorkSummary
	if err := c.doJSON(ctx, http.MethodGet, c.workPath(""), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ── Coordination mutators (the WP1 routes; the MCP's write surface) ─────────

// CreateWorkTaskRequest mirrors the platform's CreateWorkTaskRequest.
type CreateWorkTaskRequest struct {
	Prefix    string        `json:"prefix"`
	Title     string        `json:"title"`
	SpecKey   string        `json:"specKey,omitempty"`
	Milestone string        `json:"milestone,omitempty"` // v4: lands inside SpecKey's ladder
	Contract  *WorkContract `json:"contract,omitempty"`
}

// WorkMutationResponse reports the appended coordination event's seq.
type WorkMutationResponse struct {
	Key string `json:"key"`
	Seq int64  `json:"seq"`
}

// CreateWorkTask creates a task through the one mutator surface.
func (c *Client) CreateWorkTask(ctx context.Context, req CreateWorkTaskRequest) (*WorkMutationResponse, error) {
	var resp WorkMutationResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/tasks"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CommentWork appends a comment_added event.
func (c *Client) CommentWork(ctx context.Context, key, body string) (*WorkMutationResponse, error) {
	var resp WorkMutationResponse
	req := struct {
		Body string `json:"body"`
	}{Body: body}
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/tasks/"+urlSegment(key)+"/comment"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// AssignWork appends an assigned/unassigned event for a membership subject.
func (c *Client) AssignWork(ctx context.Context, key, subject string, unassign bool) (*WorkMutationResponse, error) {
	var resp WorkMutationResponse
	req := struct {
		Subject  string `json:"subject"`
		Unassign bool   `json:"unassign,omitempty"`
	}{Subject: subject, Unassign: unassign}
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/tasks/"+urlSegment(key)+"/assign"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// EditWorkContract appends a contract_edited event.
func (c *Client) EditWorkContract(ctx context.Context, key string, contract WorkContract) (*WorkMutationResponse, error) {
	var resp WorkMutationResponse
	req := struct {
		Contract WorkContract `json:"contract"`
	}{Contract: contract}
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/tasks/"+urlSegment(key)+"/contract"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// EditWorkItemRequest mirrors the platform's EditWorkItemRequest — envelope
// edits only (title/description/labels + the v4 properties). Nothing here can
// move a rung; lifecycle stays derived (WP-3). Pointer fields distinguish
// "leave unchanged" (nil) from "clear/unfile" (explicit null on the wire).
type EditWorkItemRequest struct {
	Title           *string           `json:"title,omitempty"`
	Description     *string           `json:"description,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	Initiative      *string           `json:"initiative,omitempty"`
	TargetDate      *string           `json:"targetDate,omitempty"`
	Owner           *string           `json:"owner,omitempty"`
	SuccessCriteria []string          `json:"successCriteria,omitempty"`
	// IS-L (design §15): the last coordination seq the caller read for this
	// item. A lost race answers 409 with the winning seq and current value —
	// a legible retry, never a silent overwrite. The skills teach: send it.
	IfSeq *int64 `json:"ifSeq,omitempty"`
}

// EditWorkItem edits an item's envelope through the one mutator (item_edited).
// Works for tasks, epics/specs, and initiatives — the fold reads whichever
// fields the kind exposes.
func (c *Client) EditWorkItem(ctx context.Context, key string, req EditWorkItemRequest) (*WorkMutationResponse, error) {
	var resp WorkMutationResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/items/"+urlSegment(key)+"/edit"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CancelWorkItem retires an item that carries a lifecycle — a task's rung or
// an epic's intent — through the kind-agnostic items path. Cancel is the
// model's native "delete": a terminal, attributed, append-only state, never a
// row removal. The server rejects initiatives (no lifecycle to cancel).
func (c *Client) CancelWorkItem(ctx context.Context, key string) (*WorkMutationResponse, error) {
	var resp WorkMutationResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/items/"+urlSegment(key)+"/cancel"), struct{}{}, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ── Read-only v3 surfaces the MCP exposes (PM5) ──────────────────────────────

// WorkTimelineEntry is one interleaved entry of the two logs — a
// coordination event or an observation, by time. Payloads stay raw: the MCP
// hands them to the agent verbatim; nothing here is interpreted client-side.
type WorkTimelineEntry struct {
	At          string          `json:"at"`
	Type        string          `json:"type"` // "event" | "observation"
	Event       json.RawMessage `json:"event,omitempty"`
	Observation json.RawMessage `json:"observation,omitempty"`
}

// WorkTimeline is the unified timeline for one item (PM1 route).
type WorkTimeline struct {
	Key     string              `json:"key"`
	Entries []WorkTimelineEntry `json:"entries"`
}

// GetWorkTimeline fetches both logs interleaved for one task/spec key.
func (c *Client) GetWorkTimeline(ctx context.Context, key string) (*WorkTimeline, error) {
	var resp WorkTimeline
	if err := c.doJSON(ctx, http.MethodGet, c.workPath("/timeline/"+urlSegment(key)), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// WorkDoc is one content-addressed cloud document revision (V3-2: the
// digest form equals the imported doc_ref).
type WorkDoc struct {
	Revision  string `json:"revision"`
	Parent    string `json:"parent,omitempty"`
	SpecKey   string `json:"specKey"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
}

// WorkMilestoneView mirrors the platform's milestone view: authored fields
// plus DERIVED progress (never entered — V4-4).
type WorkMilestoneView struct {
	Key        string         `json:"key"`
	Title      string         `json:"title"`
	Goal       string         `json:"goal,omitempty"`
	DoneWhen   []string       `json:"doneWhen,omitempty"`
	TargetDate string         `json:"targetDate,omitempty"`
	Ordinal    int            `json:"ordinal"`
	Progress   map[string]int `json:"progress,omitempty"`
	Total      int            `json:"total,omitempty"`
	Complete   int            `json:"complete,omitempty"`
}

// WorkMilestonesView is one epic's ladder with derived progress.
type WorkMilestonesView struct {
	Epic        string              `json:"epic"`
	Milestones  []WorkMilestoneView `json:"milestones"`
	Unscheduled *struct {
		Total    int `json:"total"`
		Complete int `json:"complete"`
	} `json:"unscheduled,omitempty"`
}

// GetEpicMilestones fetches an epic's milestone ladder with derived
// per-milestone progress.
func (c *Client) GetEpicMilestones(ctx context.Context, epicKey string) (*WorkMilestonesView, error) {
	var resp WorkMilestonesView
	if err := c.doJSON(ctx, http.MethodGet, c.workPath("/epics/"+urlSegment(epicKey)+"/milestones"), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// WorkDesignView mirrors the platform's design view: the doc chain pointer,
// the sealed context, the structured proposal, and the FOLDED intent state.
type WorkDesignView struct {
	Key        string          `json:"key"`
	Initiative string          `json:"initiative"`
	Title      string          `json:"title"`
	DocRef     string          `json:"docRef,omitempty"`
	Context    json.RawMessage `json:"context,omitempty"`
	Proposal   json.RawMessage `json:"proposal,omitempty"`
	CreatedBy  WorkActor       `json:"createdBy"`
	CreatedAt  string          `json:"createdAt,omitempty"`
	Intent     json.RawMessage `json:"intent,omitempty"`
}

// GetWorkDesign fetches one design.
func (c *Client) GetWorkDesign(ctx context.Context, key string) (*WorkDesignView, error) {
	var resp WorkDesignView
	if err := c.doJSON(ctx, http.MethodGet, c.workPath("/designs/"+urlSegment(key)), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CreateWorkDesignRequest mirrors the platform's CreateWorkDesignRequest —
// a design is a PROPOSAL (agents may author one); adoption stays human-only.
type CreateWorkDesignRequest struct {
	Title    string          `json:"title"`
	DocRef   string          `json:"docRef,omitempty"`
	Proposal json.RawMessage `json:"proposal,omitempty"`
	Catalog  string          `json:"catalog,omitempty"`
}

// CreateWorkDesign creates a Draft design under an initiative; the cloud
// seals the context (catalog digest + log cursors) server-side.
func (c *Client) CreateWorkDesign(ctx context.Context, initiativeKey string, req CreateWorkDesignRequest) (*WorkMutationResponse, error) {
	var resp WorkMutationResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/initiatives/"+urlSegment(initiativeKey)+"/designs"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// WorkRollups mirrors the platform's initiative rollup: derived health with
// named evidence plus per-epic intent + execution.
type WorkRollups struct {
	Initiative string          `json:"initiative"`
	Health     string          `json:"health"`
	Evidence   []string        `json:"evidence,omitempty"`
	Progress   map[string]int  `json:"progress"`
	Total      int             `json:"total"`
	Complete   int             `json:"complete"`
	Epics      json.RawMessage `json:"epics"`
}

// GetWorkRollups fetches one initiative's derived rollup.
func (c *Client) GetWorkRollups(ctx context.Context, initiativeKey string) (*WorkRollups, error) {
	var resp WorkRollups
	if err := c.doJSON(ctx, http.MethodGet, c.workPath("/rollups?initiative="+urlSegment(initiativeKey)), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RegenerateWorkTasksRequest mirrors the platform's request: the replacement
// plan for one milestone. Planned tasks cancel; in-flight tasks survive.
type RegenerateWorkTasksRequest struct {
	Tasks []struct {
		Title    string        `json:"title"`
		Contract *WorkContract `json:"contract,omitempty"`
	} `json:"tasks"`
	Prefix string `json:"prefix,omitempty"`
}

// RegenerateWorkTasksResponse reports the batch's one verdict.
type RegenerateWorkTasksResponse struct {
	Canceled []string `json:"canceled"`
	Kept     []string `json:"kept"`
	Created  []string `json:"created"`
}

// RegenerateWorkTasks re-plans one milestone in one verdict batch.
func (c *Client) RegenerateWorkTasks(ctx context.Context, epicKey, milestone string, req RegenerateWorkTasksRequest) (*RegenerateWorkTasksResponse, error) {
	var resp RegenerateWorkTasksResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/epics/"+urlSegment(epicKey)+"/milestones/"+urlSegment(milestone)+"/regenerate"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// WorkEpicBrief is the sealed brief approval minted (orun-work-v4 WH4):
// canonical bytes plus their content id. Verification is content addressing
// itself — sha256(Canonical) MUST equal ID; there is no second canonicalizer.
type WorkEpicBrief struct {
	ID        string `json:"id"`
	Subject   string `json:"subject"`
	Canonical string `json:"canonical"`
	CreatedAt string `json:"createdAt,omitempty"`
}

// GetEpicBrief fetches the sealed EpicSnapshot for an epic — latest when id
// is empty, or the exact pinned snapshot.
func (c *Client) GetEpicBrief(ctx context.Context, epicKey, id string) (*WorkEpicBrief, error) {
	path := c.workPath("/epics/" + urlSegment(epicKey) + "/brief")
	if id != "" {
		path += "?id=" + urlSegment(id)
	}
	var resp WorkEpicBrief
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetWorkDoc fetches a spec's cloud document (latest when rev is empty).
func (c *Client) GetWorkDoc(ctx context.Context, specKey, rev string) (*WorkDoc, error) {
	path := c.workPath("/specs/" + urlSegment(specKey) + "/doc")
	if rev != "" {
		path += "?rev=" + urlSegment(rev)
	}
	var resp WorkDoc
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ── orun-initiatives (IN1–IN2) — the four derived reads the Initiatives
// surface renders: portfolio, tree, task detail, tagged activity. Wire
// shapes mirror @saas/contracts/work (the field-level truth); everything
// here is a fold over the two logs — nothing is stored, nothing writable.

// WorkFoldStats is the portfolio header's figures. AgentsLive is optional
// on the wire — the work plane does not own sessions.
type WorkFoldStats struct {
	OpenTasks  int `json:"openTasks"`
	NeedsYou   int `json:"needsYou"`
	AgentsLive int `json:"agentsLive,omitempty"`
}

// WorkNeedsYouReason is one reason an initiative waits on a human — always
// with the item key it points at and a short server sentence.
type WorkNeedsYouReason struct {
	Kind    string `json:"kind"` // approval_drifted | awaiting_approval | review_open | milestone_idle | design_in_review
	Subject string `json:"subject"`
	Text    string `json:"text"`
}

// WorkProgressView is the two-segment meter arithmetic: done = Done+Released,
// active = In Progress+In Review, total = all non-canceled member tasks.
type WorkProgressView struct {
	Done   int `json:"done"`
	Active int `json:"active"`
	Total  int `json:"total"`
}

// WorkApprovalView mirrors the platform's approval record on an epic intent.
type WorkApprovalView struct {
	Revision   string    `json:"revision,omitempty"`
	Snapshot   string    `json:"snapshot,omitempty"`
	By         WorkActor `json:"by"`
	At         string    `json:"at,omitempty"`
	LadderHash string    `json:"ladderHash,omitempty"`
}

// WorkEpicIntentView is the folded intent ladder: state plus the approval
// record and drift flags (approved never renders without its revision).
type WorkEpicIntentView struct {
	State           string            `json:"state"`
	Approval        *WorkApprovalView `json:"approval,omitempty"`
	CurrentRevision string            `json:"currentRevision,omitempty"`
	DocDrifted      bool              `json:"docDrifted,omitempty"`
	LadderDrifted   bool              `json:"ladderDrifted,omitempty"`
}

// WorkPortfolioEpicRow is one epic row inside a portfolio initiative.
type WorkPortfolioEpicRow struct {
	Key            string             `json:"key"`
	Title          string             `json:"title"`
	Intent         WorkEpicIntentView `json:"intent"`
	Progress       WorkProgressView   `json:"progress"`
	ProposedBy     string             `json:"proposedBy,omitempty"`
	AgentAssignees []string           `json:"agentAssignees"`
}

// WorkPortfolioDesignRow is one initiative-level design run in the portfolio.
type WorkPortfolioDesignRow struct {
	Key                string `json:"key"`
	Title              string `json:"title"`
	State              string `json:"state"` // draft | in_review | adopted | superseded
	ProposedEpics      int    `json:"proposedEpics"`
	ProposedMilestones int    `json:"proposedMilestones"`
}

// WorkPortfolioInitiativeRow is one initiative in the portfolio fold.
type WorkPortfolioInitiativeRow struct {
	Key            string                   `json:"key"`
	Title          string                   `json:"title"`
	Status         string                   `json:"status"` // planning | on_track | at_risk | off_track
	HealthEvidence []string                 `json:"healthEvidence,omitempty"`
	Owner          string                   `json:"owner,omitempty"`
	TargetDate     string                   `json:"targetDate,omitempty"`
	EpicCount      int                      `json:"epicCount"`
	Progress       WorkProgressView         `json:"progress"`
	NeedsYou       []WorkNeedsYouReason     `json:"needsYou"`
	AgentAssignees []string                 `json:"agentAssignees"`
	Epics          []WorkPortfolioEpicRow   `json:"epics"`
	Designs        []WorkPortfolioDesignRow `json:"designs"`
}

// WorkPortfolio is the Initiatives home in one read (WorkPortfolioResponse).
type WorkPortfolio struct {
	Stats       WorkFoldStats                `json:"stats"`
	Initiatives []WorkPortfolioInitiativeRow `json:"initiatives"`
	CoordSeq    int64                        `json:"coordSeq"`
	ObsSeq      int64                        `json:"obsSeq"`
}

// ListInitiatives fetches the portfolio: fold-stats plus one row per
// initiative with progress, needs-you reasons, epic and design rows.
func (c *Client) ListInitiatives(ctx context.Context) (*WorkPortfolio, error) {
	var resp WorkPortfolio
	if err := c.doJSON(ctx, http.MethodGet, c.workPath("/initiatives"), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// WorkTaskEvidenceView is the task-rail evidence fold: branch_seen → branch,
// pr_* → pr, gate_result → checks. Fields absent when the logs are silent —
// evidence is never invented.
type WorkTaskEvidenceView struct {
	Branch *WorkEvidenceBranch `json:"branch,omitempty"`
	PR     *WorkEvidencePR     `json:"pr,omitempty"`
	Checks *WorkEvidenceChecks `json:"checks,omitempty"`
}

// WorkEvidenceBranch is the branch leg of the evidence fold.
type WorkEvidenceBranch struct {
	Name       string `json:"name"`
	LastPushAt string `json:"lastPushAt,omitempty"`
}

// WorkEvidencePR is the pull-request leg of the evidence fold.
type WorkEvidencePR struct {
	Number       string `json:"number"`
	Merged       bool   `json:"merged"`
	MergedAt     string `json:"mergedAt,omitempty"`
	ChecksPassed int    `json:"checksPassed,omitempty"`
	ChecksTotal  int    `json:"checksTotal,omitempty"`
}

// WorkEvidenceChecks is the gate_result leg of the evidence fold.
type WorkEvidenceChecks struct {
	Passed int    `json:"passed"`
	Total  int    `json:"total"`
	At     string `json:"at,omitempty"`
}

// WorkTreeTaskRow is one task row in the initiative tree.
type WorkTreeTaskRow struct {
	Key      string               `json:"key"`
	Title    string               `json:"title"`
	Rung     string               `json:"rung"`
	Assignee *WorkActor           `json:"assignee,omitempty"`
	Evidence WorkTaskEvidenceView `json:"evidence"`
	LandedAt string               `json:"landedAt,omitempty"`
}

// WorkTreeMilestone is one ladder milestone with its derived state
// (complete | active | upcoming — pure ladder arithmetic) and member tasks.
type WorkTreeMilestone struct {
	Key      string            `json:"key"`
	Title    string            `json:"title"`
	Goal     string            `json:"goal,omitempty"`
	DoneWhen []string          `json:"doneWhen,omitempty"`
	State    string            `json:"state"`
	Progress WorkProgressView  `json:"progress"`
	Tasks    []WorkTreeTaskRow `json:"tasks"`
}

// WorkTreeDocRow is one document row (epic spec or design doc) in the tree.
type WorkTreeDocRow struct {
	Subject  string     `json:"subject"`
	Kind     string     `json:"kind"` // spec | design
	Title    string     `json:"title"`
	Revision string     `json:"revision,omitempty"`
	State    string     `json:"state"` // approved | drifted | adopted | archived | draft
	Author   *WorkActor `json:"author,omitempty"`
	Threads  *struct {
		Total int `json:"total"`
		Open  int `json:"open"`
	} `json:"threads,omitempty"`
}

// WorkTreeEpic is one epic subtree: intent, milestones, backlog, docs.
type WorkTreeEpic struct {
	Key         string              `json:"key"`
	Title       string              `json:"title"`
	Description string              `json:"description,omitempty"`
	Intent      WorkEpicIntentView  `json:"intent"`
	Owner       string              `json:"owner,omitempty"`
	TargetDate  string              `json:"targetDate,omitempty"`
	Progress    WorkProgressView    `json:"progress"`
	Milestones  []WorkTreeMilestone `json:"milestones"`
	Backlog     []WorkTreeTaskRow   `json:"backlog"`
	Docs        []WorkTreeDocRow    `json:"docs"`
}

// WorkTreeInitiative is the tree's initiative header: the initiative view
// plus the portfolio's status/needs-you/progress folds.
type WorkTreeInitiative struct {
	WorkInitiativeView
	Status       string               `json:"status"`
	NeedsYou     []WorkNeedsYouReason `json:"needsYou"`
	ProgressView WorkProgressView     `json:"progressView"`
}

// WorkInitiativeTree is one initiative's whole world
// (WorkInitiativeTreeResponse): the epic page and the home expansion.
type WorkInitiativeTree struct {
	Initiative WorkTreeInitiative `json:"initiative"`
	Epics      []WorkTreeEpic     `json:"epics"`
	Designs    []WorkDesignView   `json:"designs"`
}

// GetInitiativeTree fetches one initiative's full hierarchy: epics with
// intent, milestones with derived state, tasks with rungs and evidence,
// docs, and the initiative-scoped design runs. 404 (never 403) on
// cross-tenant or missing.
func (c *Client) GetInitiativeTree(ctx context.Context, key string) (*WorkInitiativeTree, error) {
	var resp WorkInitiativeTree
	if err := c.doJSON(ctx, http.MethodGet, c.workPath("/initiatives/"+urlSegment(key)), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// WorkItemRef names an ancestor item (initiative/epic/milestone) on a task.
type WorkItemRef struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

// WorkComponentTouched is a diffstat carried by an observation payload;
// empty when the world never reported one — never invented.
type WorkComponentTouched struct {
	Path      string `json:"path"`
	Additions int    `json:"additions,omitempty"`
	Deletions int    `json:"deletions,omitempty"`
	Summary   string `json:"summary,omitempty"`
}

// WorkActivityEntry is one entry of the folded two-log tail. Actor is
// absent for observations — the world acted, not an actor.
type WorkActivityEntry struct {
	At      string     `json:"at"`
	Source  string     `json:"source"` // coordination | observation
	Kind    string     `json:"kind"`
	Subject string     `json:"subject"`
	Tag     string     `json:"tag"`
	Actor   *WorkActor `json:"actor,omitempty"`
	Text    string     `json:"text"`
}

// WorkTaskDetail is one task's whole page (WorkTaskDetailResponse): the
// task view, its ancestry, evidence, components touched, and activity tail.
type WorkTaskDetail struct {
	Task               WorkTaskView           `json:"task"`
	Initiative         *WorkItemRef           `json:"initiative,omitempty"`
	Epic               *WorkItemRef           `json:"epic,omitempty"`
	Milestone          *WorkItemRef           `json:"milestone,omitempty"`
	Evidence           WorkTaskEvidenceView   `json:"evidence"`
	ComponentsAffected []WorkComponentTouched `json:"componentsAffected"`
	Activity           []WorkActivityEntry    `json:"activity"`
}

// GetTaskDetail fetches one task's detail: rung with evidence, ancestry,
// components affected, and the task-scoped activity tail (newest first).
func (c *Client) GetTaskDetail(ctx context.Context, key string) (*WorkTaskDetail, error) {
	var resp WorkTaskDetail
	if err := c.doJSON(ctx, http.MethodGet, c.workPath("/tasks/"+urlSegment(key)), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// WorkActivityOptions filters the tagged activity tail. Empty fields are
// omitted. Tag ancestry is server-side: filtering by an epic covers its
// milestones' tasks, docs, and designs; an initiative covers its subtree.
// Narration is exclude|include|only (IS3; server default exclude);
// After/WaitSeconds ride the IS-Q long-poll bridge.
type WorkActivityOptions struct {
	Tag         string
	Limit       int
	Cursor      string
	Narration   string
	After       int64
	WaitSeconds int
}

// WorkActivity is the reverse-chronological tail (WorkActivityResponse).
// Seq is the long-poll watermark.
type WorkActivity struct {
	Entries    []WorkActivityEntry `json:"entries"`
	NextCursor string              `json:"nextCursor,omitempty"`
	Seq        int64               `json:"seq,omitempty"`
}

// GetWorkActivity fetches the tagged activity tail: both logs folded into
// one reverse-chronological list of neutral server sentences.
func (c *Client) GetWorkActivity(ctx context.Context, opts WorkActivityOptions) (*WorkActivity, error) {
	q := url.Values{}
	if opts.Tag != "" {
		q.Set("tag", opts.Tag)
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		q.Set("cursor", opts.Cursor)
	}
	if opts.Narration != "" {
		q.Set("narration", opts.Narration)
	}
	setLongPoll(q, opts.After, opts.WaitSeconds)
	path := c.workPath("/activity")
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var resp WorkActivity
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CreateWorkInitiativeRequest mirrors the platform's
// CreateWorkInitiativeRequest — the strategic envelope. No lifecycle, no
// contract: an initiative has nothing to cancel and nothing to fold.
type CreateWorkInitiativeRequest struct {
	Slug            string   `json:"slug"`
	Title           string   `json:"title"`
	Description     string   `json:"description,omitempty"`
	Owner           string   `json:"owner,omitempty"`
	TargetDate      string   `json:"targetDate,omitempty"`
	SuccessCriteria []string `json:"successCriteria,omitempty"`
}

// CreateInitiative creates an initiative envelope through the one mutator
// surface (POST /work/initiatives).
func (c *Client) CreateInitiative(ctx context.Context, req CreateWorkInitiativeRequest) (*WorkMutationResponse, error) {
	var resp WorkMutationResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/initiatives"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// WorkDesigns is one initiative's design runs (WorkDesignsResponse).
type WorkDesigns struct {
	Designs []WorkDesignView `json:"designs"`
}

// ListWorkDesigns fetches an initiative's design runs.
func (c *Client) ListWorkDesigns(ctx context.Context, initiativeKey string) (*WorkDesigns, error) {
	var resp WorkDesigns
	if err := c.doJSON(ctx, http.MethodGet, c.workPath("/initiatives/"+urlSegment(initiativeKey)+"/designs"), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// WorkMilestoneRequest mirrors the platform's WorkMilestoneRequest: one
// ladder edit (create/edit/reorder/remove) — authored intent only; per-
// milestone progress stays derived (V4-4). Ordinal is a pointer so a
// reorder to position 0 survives the wire.
type WorkMilestoneRequest struct {
	Op         string   `json:"op"` // create | edit | reorder | remove
	Key        string   `json:"key"`
	Title      string   `json:"title,omitempty"`
	Goal       string   `json:"goal,omitempty"`
	DoneWhen   []string `json:"doneWhen,omitempty"`
	TargetDate string   `json:"targetDate,omitempty"`
	Ordinal    *int     `json:"ordinal,omitempty"`
}

// UpsertMilestones applies one ladder edit to an epic's milestone ladder
// through the one mutator (POST /work/epics/{epic}/milestones).
func (c *Client) UpsertMilestones(ctx context.Context, epicKey string, req WorkMilestoneRequest) (*WorkMutationResponse, error) {
	var resp WorkMutationResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/epics/"+urlSegment(epicKey)+"/milestones"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ── orun-initiatives-v2 (IS4) — the wire client grows: the universal
// resolver + context bundle (IS1), the stored initiative speech acts (IS2),
// the agent's voice + generalized assign (IS3), and clients for the v4
// decision endpoints that never had them. Wire shapes mirror
// @saas/contracts/work; the authoritative surface is
// specs/epics/orun-initiatives-v2/api-and-mcp.md (orun-cloud).
//
// Idempotency (IS-L): the writes whose contracts carry `clientToken` default
// it on here — when the caller supplies none, the client mints one — and ride
// doJSON's retry lane: a replayed token returns the original {key, seq} with
// replayed:true instead of acting twice, so a 5xx retry is safe by
// construction. A FRESH call still mints a fresh token and appends anew; only
// the transport retry is idempotent, not the tool call above it.

// newClientToken mints a per-attempt idempotency token (IS-L).
func newClientToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Out of entropy is not a reason to drop a write; the token is an
		// optimization (replay safety), never a correctness requirement.
		return ""
	}
	return "ct_" + hex.EncodeToString(b[:])
}

// WorkItemResolve mirrors WorkItemResolveResponse: the universal resolver's
// answer. Off-canonical hits (letterless, wrong-letter, alias, machine id)
// carry the form they arrived by in MovedFrom.
type WorkItemResolve struct {
	Kind         string `json:"kind"` // initiative | design | epic | milestone | task
	Key          string `json:"key"`
	CanonicalKey string `json:"canonicalKey"`
	PublicID     string `json:"publicId,omitempty"`
	Title        string `json:"title"`
	MovedFrom    string `json:"movedFrom,omitempty"`
}

// GetWorkItem resolves any ref — canonical key, letterless number form,
// alias, or machine id — to its typed item. 404 (never 403) on a miss.
func (c *Client) GetWorkItem(ctx context.Context, ref string) (*WorkItemResolve, error) {
	var resp WorkItemResolve
	if err := c.doJSON(ctx, http.MethodGet, c.workPath("/items/"+urlSegment(ref)), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// WorkContextOptions bounds the context bundle. Zero values take the
// server's defaults (depth 2, perLevel 50, activity 20).
type WorkContextOptions struct {
	Depth    int
	PerLevel int
	Activity int
}

// WorkContextNode is one ancestor in the bundle, nearest-first, root last,
// each with its live state word (rung / milestone state / intent state /
// initiative status).
type WorkContextNode struct {
	Kind         string `json:"kind"`
	Key          string `json:"key"`
	CanonicalKey string `json:"canonicalKey"`
	PublicID     string `json:"publicId,omitempty"`
	Title        string `json:"title"`
	State        string `json:"state,omitempty"`
}

// WorkContextBudget is the truncation echo (IS-H: no silent caps) — every
// bounded level reports what it returned out of what exists, with a cursor.
type WorkContextBudget struct {
	Level    string `json:"level"`
	Returned int    `json:"returned"`
	Total    int    `json:"total"`
	Cursor   string `json:"cursor,omitempty"`
}

// WorkContextItem names the resolved item the bundle centers on.
type WorkContextItem struct {
	Kind         string `json:"kind"`
	Key          string `json:"key"`
	CanonicalKey string `json:"canonicalKey"`
	PublicID     string `json:"publicId,omitempty"`
	Title        string `json:"title"`
}

// WorkContext is the any-key context bundle (design §5): the item's full
// view (typed by kind — handed to the agent verbatim), ancestry to the root,
// the activity tail, and open needs-you in scope. The intended first read of
// every agent session.
type WorkContext struct {
	Item           WorkContextItem      `json:"item"`
	View           json.RawMessage      `json:"view"`
	Ancestry       []WorkContextNode    `json:"ancestry"`
	Activity       []WorkActivityEntry  `json:"activity"`
	ActivityCursor string               `json:"activityCursor,omitempty"`
	NeedsYou       []WorkNeedsYouReason `json:"needsYou"`
	Budget         []WorkContextBudget  `json:"budget"`
	MovedFrom      string               `json:"movedFrom,omitempty"`
}

// GetWorkContext fetches the context bundle for any ref.
func (c *Client) GetWorkContext(ctx context.Context, ref string, opts WorkContextOptions) (*WorkContext, error) {
	q := url.Values{}
	if opts.Depth > 0 {
		q.Set("depth", strconv.Itoa(opts.Depth))
	}
	if opts.PerLevel > 0 {
		q.Set("perLevel", strconv.Itoa(opts.PerLevel))
	}
	if opts.Activity > 0 {
		q.Set("activity", strconv.Itoa(opts.Activity))
	}
	path := c.workPath("/items/" + urlSegment(ref) + "/context")
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var resp WorkContext
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SetInitiativeStatusRequest mirrors SetWorkInitiativeStatusRequest — the
// five-state machine's transition body. Force acknowledges open member tasks
// on complete (the warn never blocks).
type SetInitiativeStatusRequest struct {
	To          string `json:"to"`
	Comment     string `json:"comment,omitempty"`
	Force       bool   `json:"force,omitempty"`
	ClientToken string `json:"clientToken,omitempty"`
}

// SetInitiativeStatusResponse carries the machine's answer. An illegal move
// never reaches here — it is a 409 whose details name allowedTransitions.
type SetInitiativeStatusResponse struct {
	Key      string `json:"key"`
	Seq      int64  `json:"seq"`
	Status   string `json:"status"`
	Warning  string `json:"warning,omitempty"`
	Replayed bool   `json:"replayed,omitempty"`
}

// SetInitiativeStatus moves an initiative through the stored state machine
// (POST /work/initiatives/{key}/status). complete/cancel/reopen/restore are
// human-only server-side (IS-4, typed human_only verdict).
func (c *Client) SetInitiativeStatus(ctx context.Context, key string, req SetInitiativeStatusRequest) (*SetInitiativeStatusResponse, error) {
	if req.ClientToken == "" {
		req.ClientToken = newClientToken()
	}
	var resp SetInitiativeStatusResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/initiatives/"+urlSegment(key)+"/status"), req, &resp, req.ClientToken != ""); err != nil {
		return nil, err
	}
	return &resp, nil
}

// WorkInitiativeUpdateView is one posted update — health is never set
// directly; it is the headline of the latest update (design §2).
type WorkInitiativeUpdateView struct {
	PublicID   string    `json:"publicId"`
	Initiative string    `json:"initiative"`
	Health     string    `json:"health"`
	Body       string    `json:"body"`
	Author     WorkActor `json:"author"`
	CreatedAt  string    `json:"createdAt"`
	EditedAt   string    `json:"editedAt,omitempty"`
}

// PostInitiativeUpdateRequest mirrors PostWorkInitiativeUpdateRequest.
type PostInitiativeUpdateRequest struct {
	Health      string `json:"health"`
	Body        string `json:"body"`
	ClientToken string `json:"clientToken,omitempty"`
}

// PostInitiativeUpdateResponse carries the created update view.
type PostInitiativeUpdateResponse struct {
	Key      string                   `json:"key"`
	Seq      int64                    `json:"seq"`
	Update   WorkInitiativeUpdateView `json:"update"`
	Replayed bool                     `json:"replayed,omitempty"`
}

// PostInitiativeUpdate posts an attributed health update
// (POST /work/initiatives/{key}/updates), stamping the headline.
func (c *Client) PostInitiativeUpdate(ctx context.Context, key string, req PostInitiativeUpdateRequest) (*PostInitiativeUpdateResponse, error) {
	if req.ClientToken == "" {
		req.ClientToken = newClientToken()
	}
	var resp PostInitiativeUpdateResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/initiatives/"+urlSegment(key)+"/updates"), req, &resp, req.ClientToken != ""); err != nil {
		return nil, err
	}
	return &resp, nil
}

// WorkInitiativeUpdates is the update feed, newest first.
type WorkInitiativeUpdates struct {
	Updates []WorkInitiativeUpdateView `json:"updates"`
}

// ListInitiativeUpdates fetches an initiative's update feed.
func (c *Client) ListInitiativeUpdates(ctx context.Context, key string) (*WorkInitiativeUpdates, error) {
	var resp WorkInitiativeUpdates
	if err := c.doJSON(ctx, http.MethodGet, c.workPath("/initiatives/"+urlSegment(key)+"/updates"), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ArchiveInitiativeResponse mirrors ArchiveWorkInitiativeResponse.
type ArchiveInitiativeResponse struct {
	Key      string `json:"key"`
	Seq      int64  `json:"seq"`
	Replayed bool   `json:"replayed,omitempty"`
}

// SetInitiativeArchived archives or unarchives an initiative — a view
// concern, independent of status (design §1).
func (c *Client) SetInitiativeArchived(ctx context.Context, key string, archived bool) (*ArchiveInitiativeResponse, error) {
	action := "/archive"
	if !archived {
		action = "/unarchive"
	}
	var resp ArchiveInitiativeResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/initiatives/"+urlSegment(key)+action), struct{}{}, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// AssertTaskDone speaks the assertion lane (POST /work/tasks/{key}/done,
// design §7.1): note mandatory — an assertion without a reason is a status
// write. The fold treats it as the weakest voice; live evidence wins.
func (c *Client) AssertTaskDone(ctx context.Context, key, note, clientToken string) (*WorkMutationResponse, error) {
	if clientToken == "" {
		clientToken = newClientToken()
	}
	req := struct {
		Note        string `json:"note"`
		ClientToken string `json:"clientToken,omitempty"`
	}{Note: note, ClientToken: clientToken}
	var resp WorkMutationResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/tasks/"+urlSegment(key)+"/done"), req, &resp, clientToken != ""); err != nil {
		return nil, err
	}
	return &resp, nil
}

// PostTaskNoteRequest mirrors PostWorkTaskNoteRequest — the worklog line
// (≤280 chars server-side; per-seat clamps answer with typed rate_limited).
type PostTaskNoteRequest struct {
	Text        string `json:"text"`
	Ref         string `json:"ref,omitempty"`
	ClientToken string `json:"clientToken,omitempty"`
}

// PostTaskNote appends a worklog note (POST /work/tasks/{key}/note,
// design §7.2) — fold-inert narration; it moves nothing.
func (c *Client) PostTaskNote(ctx context.Context, key string, req PostTaskNoteRequest) (*WorkMutationResponse, error) {
	if req.ClientToken == "" {
		req.ClientToken = newClientToken()
	}
	var resp WorkMutationResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/tasks/"+urlSegment(key)+"/note"), req, &resp, req.ClientToken != ""); err != nil {
		return nil, err
	}
	return &resp, nil
}

// WorkNowOptions filters the live board. Empty fields are omitted.
// After/WaitSeconds ride the IS-Q long-poll bridge: the server holds the
// read until the logs move past After or the window (≤25s) closes.
type WorkNowOptions struct {
	Initiative  string
	Epic        string
	Seat        string
	Limit       int
	Cursor      string
	After       int64
	WaitSeconds int
}

// WorkNowLine is the live *now* line: the newest worklog note on a task.
// Age and the quiet chip derive at read, never stored.
type WorkNowLine struct {
	Text  string    `json:"text"`
	Actor WorkActor `json:"actor"`
	At    string    `json:"at"`
	Ref   string    `json:"ref,omitempty"`
}

// WorkNowRow is one row of the live board: an in-flight task × its latest
// note × the seat working it.
type WorkNowRow struct {
	Key        string       `json:"key"`
	Title      string       `json:"title"`
	Rung       string       `json:"rung"`
	Epic       string       `json:"epic,omitempty"`
	Initiative string       `json:"initiative,omitempty"`
	Seat       string       `json:"seat,omitempty"`
	Now        *WorkNowLine `json:"now,omitempty"`
	Quiet      bool         `json:"quiet"`
}

// WorkNow is the live board (WorkNowResponse), cursor-paged. Seq is the
// long-poll watermark — pass it back as After for anything newer.
type WorkNow struct {
	Rows       []WorkNowRow `json:"rows"`
	NextCursor string       `json:"nextCursor,omitempty"`
	Seq        int64        `json:"seq,omitempty"`
}

// GetWorkNow fetches the live board (GET /work/now): what every agent is
// doing right now, straight from the worklog.
func (c *Client) GetWorkNow(ctx context.Context, opts WorkNowOptions) (*WorkNow, error) {
	q := url.Values{}
	if opts.Initiative != "" {
		q.Set("initiative", opts.Initiative)
	}
	if opts.Epic != "" {
		q.Set("epic", opts.Epic)
	}
	if opts.Seat != "" {
		q.Set("seat", opts.Seat)
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		q.Set("cursor", opts.Cursor)
	}
	setLongPoll(q, opts.After, opts.WaitSeconds)
	path := c.workPath("/now")
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var resp WorkNow
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// setLongPoll emits the IS-Q params: only a positive After long-polls
// (seq 0 means "no watermark yet" — an ordinary immediate read).
func setLongPoll(q url.Values, after int64, waitSeconds int) {
	if after <= 0 {
		return
	}
	q.Set("after", strconv.FormatInt(after, 10))
	if waitSeconds > 0 {
		q.Set("waitSeconds", strconv.Itoa(waitSeconds))
	}
}

// WorkAttentionItem is AttentionItem v1 — the vendored cross-plane
// attention contract (orun-cloud specs/epics/orun-initiatives-v2/
// attention-item.md): one addressed, actionable item; Yours is the only
// renderer in the product (IS-9: no second inbox).
type WorkAttentionItem struct {
	ID      string `json:"id"`     // stable per (person, kind, subject)
	Person  string `json:"person"` // the addressee — always resolved
	Kind    string `json:"kind"`
	Subject struct {
		Key        string `json:"key"`
		PublicID   string `json:"publicId"`
		Initiative string `json:"initiative"`
	} `json:"subject"`
	Reason string `json:"reason"` // one sentence, render-ready
	Since  string `json:"since"`  // when it became actionable
	Source string `json:"source"` // work | fleet
	Act    struct {
		Tool string `json:"tool,omitempty"` // the MCP gesture that clears it
		URL  string `json:"url"`
	} `json:"act"`
}

// WorkYoursOptions pages the personal queue; After/WaitSeconds long-poll.
type WorkYoursOptions struct {
	Limit       int
	Cursor      string
	After       int64
	WaitSeconds int
}

// WorkYours is the caller's addressed queue (WorkYoursResponse),
// newest-decision-first.
type WorkYours struct {
	Items      []WorkAttentionItem `json:"items"`
	NextCursor string              `json:"nextCursor,omitempty"`
	Seq        int64               `json:"seq,omitempty"`
}

// GetWorkYours fetches the caller's addressed attention queue
// (GET /work/yours) — the daily driver: what waits on YOU, one list.
func (c *Client) GetWorkYours(ctx context.Context, opts WorkYoursOptions) (*WorkYours, error) {
	q := url.Values{}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		q.Set("cursor", opts.Cursor)
	}
	setLongPoll(q, opts.After, opts.WaitSeconds)
	path := c.workPath("/yours")
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var resp WorkYours
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// AssignWorkItemRequest mirrors the platform's AssignWorkItemRequest — the
// generalized assign (any noun; the dispatch gate still applies server-side).
type AssignWorkItemRequest struct {
	Subject     string `json:"subject"`
	Unassign    bool   `json:"unassign,omitempty"`
	Override    string `json:"override,omitempty"`
	ClientToken string `json:"clientToken,omitempty"`
}

// AssignWorkItem assigns a membership subject to any noun — designs, epics,
// initiatives (owner), tasks — through POST /work/items/{key}/assign. The
// token flows (forward-compatible), but the write stays off the retry lane
// until the assign handler replays by token server-side.
func (c *Client) AssignWorkItem(ctx context.Context, key string, req AssignWorkItemRequest) (*WorkMutationResponse, error) {
	if req.ClientToken == "" {
		req.ClientToken = newClientToken()
	}
	var resp WorkMutationResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/items/"+urlSegment(key)+"/assign"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ── The v4 decision endpoints that never had clients (review, verdict,
// approval, adoption). All shipped server-side with orun-work-v4; IS4 puts
// them on this seam so the MCP and CLI can reach them. The human-only actor
// rules live in the model layer server-side — these clients carry the typed
// human_only verdict back verbatim (IN-4). No clientToken on these bodies
// (the v4 contracts carry none), so no transport retry either.

// ReviewCollectionOf picks the wire collection a review/verdict rides for a
// key: design keys (typed `…-D<n>`, legacy `DSG-<n>`, machine `dsg_…`) go to
// /work/designs, everything else to /work/epics. Best-effort by grammar —
// the server resolves the key itself; the segment only has to route.
func ReviewCollectionOf(key string) string {
	if strings.HasPrefix(key, "dsg_") || strings.HasPrefix(key, "DSG-") {
		return "designs"
	}
	if i := strings.LastIndex(key, "-D"); i > 0 && i+2 < len(key) {
		digits := key[i+2:]
		allDigits := true
		for _, r := range digits {
			if r < '0' || r > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return "designs"
		}
	}
	return "epics"
}

// workCollection validates the collection segment a review/verdict path
// rides ("epics", "specs", or "designs" — the route accepts all three).
func workCollection(collection string) (string, error) {
	switch collection {
	case "epics", "specs", "designs":
		return collection, nil
	case "":
		return "epics", nil
	default:
		return "", fmt.Errorf("remotestate: unknown work collection %q (epics|specs|designs)", collection)
	}
}

// WorkReviewRequest mirrors the platform's WorkReviewRequest.
type WorkReviewRequest struct {
	Revision  string   `json:"revision,omitempty"`
	Reviewers []string `json:"reviewers,omitempty"`
	Note      string   `json:"note,omitempty"`
}

// RequestWorkReview requests review on an epic or a design
// (POST /work/{collection}/{key}/review).
func (c *Client) RequestWorkReview(ctx context.Context, collection, key string, req WorkReviewRequest) (*WorkMutationResponse, error) {
	col, err := workCollection(collection)
	if err != nil {
		return nil, err
	}
	var resp WorkMutationResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/"+col+"/"+urlSegment(key)+"/review"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// WorkVerdictRequest mirrors the platform's WorkVerdictRequest — an opinion
// (approve | request_changes), not a decision.
type WorkVerdictRequest struct {
	Revision string `json:"revision,omitempty"`
	Verdict  string `json:"verdict"`
	Note     string `json:"note,omitempty"`
}

// SubmitWorkVerdict submits a review verdict
// (POST /work/{collection}/{key}/verdict).
func (c *Client) SubmitWorkVerdict(ctx context.Context, collection, key string, req WorkVerdictRequest) (*WorkMutationResponse, error) {
	col, err := workCollection(collection)
	if err != nil {
		return nil, err
	}
	var resp WorkMutationResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/"+col+"/"+urlSegment(key)+"/verdict"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ApproveEpicRequest mirrors ApproveWorkEpicRequest.
type ApproveEpicRequest struct {
	Revision     string `json:"revision,omitempty"`
	MinApprovals int    `json:"minApprovals,omitempty"`
}

// ApproveEpicResponse carries the sealed EpicSnapshot's content id — the
// approval IS the dispatch artifact.
type ApproveEpicResponse struct {
	Key      string `json:"key"`
	Seq      int64  `json:"seq"`
	Snapshot string `json:"snapshot"`
}

// ApproveEpic approves an epic at a revision (POST /work/epics/{key}/approve).
// Human-only server-side; an sp_ seat gets the typed human_only verdict.
func (c *Client) ApproveEpic(ctx context.Context, key string, req ApproveEpicRequest) (*ApproveEpicResponse, error) {
	var resp ApproveEpicResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/epics/"+urlSegment(key)+"/approve"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RevokeEpicApproval revokes an epic's approval
// (POST /work/epics/{key}/revoke-approval).
func (c *Client) RevokeEpicApproval(ctx context.Context, key, note string) (*WorkMutationResponse, error) {
	req := struct {
		Note string `json:"note,omitempty"`
	}{Note: note}
	var resp WorkMutationResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/epics/"+urlSegment(key)+"/revoke-approval"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// AdoptDesignRequest mirrors AdoptWorkDesignRequest — the subset of proposal
// epics to mint (default all) and the task-key prefix for skeletons.
type AdoptDesignRequest struct {
	Epics      []string `json:"epics,omitempty"`
	TaskPrefix string   `json:"taskPrefix,omitempty"`
}

// AdoptDesignResponse names what adoption minted.
type AdoptDesignResponse struct {
	Key    string   `json:"key"`
	Seq    int64    `json:"seq"`
	Minted []string `json:"minted"`
	Tasks  []string `json:"tasks"`
}

// AdoptDesign adopts a design (POST /work/designs/{key}/adopt) — mints the
// structure and approves the minted epics at rev 0, one transaction, one
// signature. Human-only server-side.
func (c *Client) AdoptDesign(ctx context.Context, key string, req AdoptDesignRequest) (*AdoptDesignResponse, error) {
	var resp AdoptDesignResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/designs/"+urlSegment(key)+"/adopt"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SupersedeDesignRequest mirrors SupersedeWorkDesignRequest.
type SupersedeDesignRequest struct {
	By   string `json:"by,omitempty"`
	Note string `json:"note,omitempty"`
}

// SupersedeDesign supersedes a design (POST /work/designs/{key}/supersede).
func (c *Client) SupersedeDesign(ctx context.Context, key string, req SupersedeDesignRequest) (*WorkMutationResponse, error) {
	var resp WorkMutationResponse
	if err := c.doJSON(ctx, http.MethodPost, c.workPath("/designs/"+urlSegment(key)+"/supersede"), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp, nil
}
