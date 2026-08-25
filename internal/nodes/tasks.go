package nodes

import (
	"context"
	"regexp"

	"github.com/sourceplane/orun/internal/objectstore"
)

// tasks.go — the Task node (orun-tasks O1, design §3.2). The task is the
// store's first NON-DERIVABLE object (TK-9): its key is issued by the cloud
// allocator — the single writer of task identity — and everything else in
// the graph could be rebuilt from a workspace checkout, but a task ref
// records an issuance. Identity: the Merkle root of the node's tree, which
// bundles the record with the contract's sealed wire bytes so that GC
// reachability from the task ref keeps the contract an audit trail cites
// (a string-field reference would NOT be walked, and the contract would be
// collected out from under the hash the cloud stored).

// taskKeyRe is the key half of the provenance branch grammar
// (internal/provenance.BranchRe, conformance-pinned against the cloud twin);
// inlined because nodes imports no sibling internal packages.
var taskKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,5}-[A-Z]?[0-9]+$`)

// taskRefRe is the cloud's durable public handle: tsk_ + 8 Crockford base32.
var taskRefRe = regexp.MustCompile(`^tsk_[0-9A-HJKMNP-TV-Z]{8}$`)

// taskContractHashRe is the contract seal's shape (contract.ContractID).
var taskContractHashRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// TaskRecord is the task node: identity plus the seal of the contract its
// tree bundles. Deliberately NO rung, no status, no assignee — the verdict
// derives at read on the cloud (TK-M), and a tracker owns its own fields;
// a task node that carried either would be a status column in the proof
// plane.
type TaskRecord struct {
	Kind       string `json:"kind"`
	APIVersion string `json:"apiVersion"`
	// Key is the human name (ABC-123), issued by the cloud allocator and
	// immutable once issued (TK-G).
	Key string `json:"key"`
	// TaskRef is the cloud's tsk_… handle, when the task has synced.
	TaskRef string `json:"taskRef,omitempty"`
	// Title is a display mirror of the tracker's title — never ours (R4).
	Title string `json:"title,omitempty"`
	// ContractHash seals the bundled contract.json (contract.ContractID) —
	// the identity every gate decision cites. Present iff the tree carries
	// a contract entry.
	ContractHash string `json:"contractHash,omitempty"`
}

// Validate checks a TaskRecord.
func (t TaskRecord) Validate() error {
	if t.Kind != KindTask {
		return invalidf("task kind %q", t.Kind)
	}
	if t.APIVersion != apiVersionV1 {
		return invalidf("task apiVersion %q", t.APIVersion)
	}
	if !taskKeyRe.MatchString(t.Key) {
		return invalidf("task key %q", t.Key)
	}
	if t.TaskRef != "" && !taskRefRe.MatchString(t.TaskRef) {
		return invalidf("task ref %q", t.TaskRef)
	}
	if t.ContractHash != "" && !taskContractHashRe.MatchString(t.ContractHash) {
		return invalidf("task contractHash %q", t.ContractHash)
	}
	return nil
}

// AssembleTask writes the task node: the record blob plus, when the task
// carries a contract, the contract's canonical wire bytes as their own blob
// — bundled into one tree whose Merkle root is the node's identity. The
// record's ContractHash and the presence of contract bytes must agree; a
// seal without bytes (or bytes without a seal) is refused rather than
// assembled into a tree that lies about what it roots.
func AssembleTask(ctx context.Context, s store, t TaskRecord, contractWire []byte) (ObjectID, error) {
	t.Kind = KindTask
	t.APIVersion = apiVersionV1
	if (t.ContractHash == "") != (len(contractWire) == 0) {
		return "", invalidf("task %q: contractHash and contract bytes must be present together", t.Key)
	}
	if err := t.Validate(); err != nil {
		return "", err
	}
	rec, err := Encode(t)
	if err != nil {
		return "", err
	}
	recID, err := s.PutBlob(ctx, rec)
	if err != nil {
		return "", err
	}
	entries := []objectstore.TreeEntry{blobEntry(fileTask, recID)}
	if len(contractWire) > 0 {
		contractID, err := s.PutBlob(ctx, contractWire)
		if err != nil {
			return "", err
		}
		entries = append(entries, blobEntry(fileTaskContract, contractID))
	}
	return s.PutTree(ctx, entries)
}
