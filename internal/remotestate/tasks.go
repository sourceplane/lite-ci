package remotestate

import (
	"context"
	"encoding/json"
	"net/http"
)

// Tasks client (orun-tasks O2) — the CLI's door to the task plane:
// `/v1/organizations/{org}/tasks…` through the api-edge facade. Create is
// the ONE identity write (the cloud allocator is the single writer of task
// identity, TK-I; the CLI never invents a key), attach uploads a contract
// the author sealed locally (the server recomputes the hash and refuses a
// mismatch, TK-J), and every read mirrors the wire types in
// orun-cloud packages/contracts/src/tasks.ts. The verdict is derived at
// read on the server (TK-M) — nothing here caches or stores one.

// PublicTask is a task as every read surface sees it (PublicTask on the
// wire): identity plus the R4 display mirror. Nullable wire fields decode
// to "" — absence, not a value.
type PublicTask struct {
	// ID is the durable tsk_… handle.
	ID        string `json:"id"`
	Key       string `json:"key"`
	KeyOrigin string `json:"keyOrigin"`
	// TaskRef is the object-store ref, once synced from the CLI side.
	TaskRef string `json:"taskRef"`
	// TitleMirror mirrors the tracker's title — never ours (R4).
	TitleMirror string `json:"titleMirror"`
	SyncedAt    string `json:"syncedAt"`
	CreatedBy   string `json:"createdBy"`
	CreatedAt   string `json:"createdAt"`
	CanceledAt  string `json:"canceledAt"`
	// ContractHash is present when a contract is attached — the identity
	// every gate decision cites.
	ContractHash string `json:"contractHash"`
}

// TaskDerive mirrors CreateTaskRequest.derive: repo prefix + issue number
// for DERIVED keys (`web#123` → `WEB-123`).
type TaskDerive struct {
	RepoPrefix  string `json:"repoPrefix"`
	IssueNumber int    `json:"issueNumber"`
}

// TaskCreateRequest mirrors CreateTaskRequest. The allocator is the single
// writer: adopt only if free, else derive, else the sequence — never a
// client-side choice.
type TaskCreateRequest struct {
	AdoptKey    string      `json:"adoptKey,omitempty"`
	Derive      *TaskDerive `json:"derive,omitempty"`
	MintPrefix  string      `json:"mintPrefix,omitempty"`
	TitleMirror string      `json:"titleMirror,omitempty"`
}

// TasksList mirrors ListTasksResponse.
type TasksList struct {
	Tasks []PublicTask `json:"tasks"`
}

// TaskContractSeal mirrors AttachContractResponse: the stored identity —
// always the server's own recomputation.
type TaskContractSeal struct {
	ContractHash string `json:"contractHash"`
	SyncedAt     string `json:"syncedAt"`
}

// TaskContractView mirrors GetContractResponse. The body stays raw bytes on
// this seam: remotestate carries the wire, internal/contract owns the type.
type TaskContractView struct {
	ContractHash string          `json:"contractHash"`
	Contract     json.RawMessage `json:"contract"`
	SyncedAt     string          `json:"syncedAt"`
}

// TaskVerdict mirrors TaskVerdictWire — the derived rung plus the evidence
// that produced it (a rung without a reason is a status column in costume).
type TaskVerdict struct {
	Rung     string `json:"rung"`
	Evidence struct {
		ObservationID string `json:"observationId"`
		Reason        string `json:"reason"`
	} `json:"evidence"`
	Pin *struct {
		Rung   string `json:"rung"`
		Active bool   `json:"active"`
	} `json:"pin"`
	Dissent *struct {
		Asserted string `json:"asserted"`
	} `json:"dissent"`
	Blocked   bool     `json:"blocked"`
	BlockedBy []string `json:"blockedBy"`
}

// TaskDependency is one resolved contract dep (GetVerdictResponse).
type TaskDependency struct {
	Ref   string `json:"ref"`
	State string `json:"state"` // open | done | canceled | unknown
}

// TaskObservation is one observed fact (TaskObservationWire) — evidence,
// never status.
type TaskObservation struct {
	ID         string                 `json:"id"`
	Kind       string                 `json:"kind"`
	OccurredAt string                 `json:"occurredAt"`
	Payload    map[string]interface{} `json:"payload"`
}

// TaskVerdictView mirrors GetVerdictResponse: the verdict plus the evidence
// it cites, resolved in the same read.
type TaskVerdictView struct {
	Verdict      TaskVerdict       `json:"verdict"`
	ContractHash string            `json:"contractHash"`
	Dependencies []TaskDependency  `json:"dependencies"`
	Observations []TaskObservation `json:"observations"`
}

func tasksPathFor(org, suffix string) string {
	return orgPath(org, "/tasks"+suffix)
}

// CreateTask asks the allocator for a task (adopt > derive > mint — the
// server's ladder, not ours). POSTs are not retried here: the caller decides
// what a second attempt means.
func (c *Client) CreateTask(ctx context.Context, org string, req TaskCreateRequest) (*PublicTask, error) {
	var resp struct {
		Task PublicTask `json:"task"`
	}
	if err := c.doJSON(ctx, http.MethodPost, tasksPathFor(org, ""), req, &resp, false); err != nil {
		return nil, err
	}
	return &resp.Task, nil
}

// ListTasks fetches the org's tasks.
func (c *Client) ListTasks(ctx context.Context, org string) (*TasksList, error) {
	var resp TasksList
	if err := c.doJSON(ctx, http.MethodGet, tasksPathFor(org, ""), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetTask fetches one task by key (ENG-42) or durable id (tsk_…).
func (c *Client) GetTask(ctx context.Context, org, keyOrID string) (*PublicTask, error) {
	var resp struct {
		Task PublicTask `json:"task"`
	}
	if err := c.doJSON(ctx, http.MethodGet, tasksPathFor(org, "/"+urlSegment(keyOrID)), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp.Task, nil
}

// AttachTaskContract uploads a contract's canonical wire bytes plus the hash
// the author sealed; the server recomputes and refuses a mismatch, so a
// corrupted body never becomes the projection. Retried freely: attaching
// identical content is idempotent by construction.
func (c *Client) AttachTaskContract(ctx context.Context, org, keyOrID string, contractWire json.RawMessage, contractHash string) (*TaskContractSeal, error) {
	req := struct {
		Contract     json.RawMessage `json:"contract"`
		ContractHash string          `json:"contractHash,omitempty"`
	}{Contract: contractWire, ContractHash: contractHash}
	var resp TaskContractSeal
	if err := c.doJSON(ctx, http.MethodPut, tasksPathFor(org, "/"+urlSegment(keyOrID)+"/contract"), req, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetTaskContract fetches the attached contract.
func (c *Client) GetTaskContract(ctx context.Context, org, keyOrID string) (*TaskContractView, error) {
	var resp TaskContractView
	if err := c.doJSON(ctx, http.MethodGet, tasksPathFor(org, "/"+urlSegment(keyOrID)+"/contract"), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetTaskVerdict fetches the derived verdict plus its evidence.
func (c *Client) GetTaskVerdict(ctx context.Context, org, keyOrID string) (*TaskVerdictView, error) {
	var resp TaskVerdictView
	if err := c.doJSON(ctx, http.MethodGet, tasksPathFor(org, "/"+urlSegment(keyOrID)+"/verdict"), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}
