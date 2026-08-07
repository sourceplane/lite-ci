package remotestate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
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
type WorkActivityOptions struct {
	Tag    string
	Limit  int
	Cursor string
}

// WorkActivity is the reverse-chronological tail (WorkActivityResponse).
type WorkActivity struct {
	Entries    []WorkActivityEntry `json:"entries"`
	NextCursor string              `json:"nextCursor,omitempty"`
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
