// Package taskobj seals tasks into the object graph and reads them back
// (orun-tasks O1, design §3.2). The task ref (refs/tasks/<key>) is the
// store's first non-derivable root (TK-9): everything else in the graph can
// be rebuilt from a checkout, but a task records an issuance by the cloud
// allocator. The ref roots the node's tree, and the tree bundles the
// contract's sealed wire bytes — which is what keeps a contract reachable
// through GC for as long as anything cites its hash (reachability marks from
// refs and walks TREES; a contract referenced only by a string field would
// be swept).
//
// The internal/agent/seal.go posture, applied to tasks: assemble the node,
// then create-or-CAS-move the ref. Idempotent — re-sealing identical content
// moves nothing.
package taskobj

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sourceplane/orun/internal/contract"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objplan"
)

// SealInput is a task as the CLI learned it from the cloud create (key,
// tsk_ ref, mirrored title) plus the locally authored contract, if any.
type SealInput struct {
	Key     string
	TaskRef string
	Title   string
	// Contract is the authored contract to seal into the node's tree; nil
	// seals a task with no contract (no narrowing — TK-4's honest state).
	Contract *contract.Contract
}

// SealTask assembles the task node and points refs/tasks/<key> at it.
// Returns the node id and the contract's sealed hash ("" when no contract) —
// the same hash the cloud verifies and stores on attach, byte-pinned by the
// contract package's conformance fixture.
func SealTask(ctx context.Context, store objectstore.ObjectStore, refs refstore.RefStore, in SealInput) (objectstore.ObjectID, string, error) {
	rec := nodes.TaskRecord{
		Key:     in.Key,
		TaskRef: in.TaskRef,
		Title:   in.Title,
	}
	var wire []byte
	var hash string
	if in.Contract != nil {
		var err error
		hash, wire, err = contract.ContractID(in.Contract)
		if err != nil {
			return "", "", err
		}
		rec.ContractHash = hash
	}
	id, err := nodes.AssembleTask(ctx, store, rec, wire)
	if err != nil {
		return "", "", err
	}
	name := objplan.TaskRefs(in.Key)
	cur, rErr := refs.Read(ctx, name)
	switch {
	case rErr == nil:
		if cur.Target != string(id) {
			if err := refs.Update(ctx, name, cur.Target, string(id)); err != nil {
				return "", "", fmt.Errorf("taskobj: move task ref: %w", err)
			}
		}
	default:
		if err := refs.Update(ctx, name, "", string(id)); err != nil {
			return "", "", fmt.Errorf("taskobj: create task ref: %w", err)
		}
	}
	return id, hash, nil
}

// ReadTask resolves refs/tasks/<key> and returns the record plus the sealed
// contract, verified: the bundled bytes must hash to the record's
// contractHash, or the read refuses — a tree that lies about what it roots
// is worse than a missing one.
func ReadTask(ctx context.Context, store objectstore.ObjectStore, refs refstore.RefStore, key string) (nodes.TaskRecord, *contract.Contract, error) {
	ref, err := refs.Read(ctx, objplan.TaskRefs(key))
	if err != nil {
		return nodes.TaskRecord{}, nil, fmt.Errorf("taskobj: task %q: %w", key, err)
	}
	entries, err := store.GetTree(ctx, objectstore.ObjectID(ref.Target))
	if err != nil {
		return nodes.TaskRecord{}, nil, fmt.Errorf("taskobj: task %q tree: %w", key, err)
	}
	var rec nodes.TaskRecord
	var wire []byte
	for _, e := range entries {
		switch e.Name {
		case "task.json":
			_, body, err := store.Get(ctx, e.ID)
			if err != nil {
				return nodes.TaskRecord{}, nil, err
			}
			rec, err = nodes.Decode[nodes.TaskRecord](body)
			if err != nil {
				return nodes.TaskRecord{}, nil, err
			}
		case "contract.json":
			_, body, err := store.Get(ctx, e.ID)
			if err != nil {
				return nodes.TaskRecord{}, nil, err
			}
			wire = body
		}
	}
	if rec.Key == "" {
		return nodes.TaskRecord{}, nil, fmt.Errorf("taskobj: task %q tree carries no task.json", key)
	}
	if rec.ContractHash == "" {
		return rec, nil, nil
	}
	var c contract.Contract
	if err := json.Unmarshal(wire, &c); err != nil {
		return nodes.TaskRecord{}, nil, fmt.Errorf("taskobj: task %q contract: %w", key, err)
	}
	sealed, _, err := contract.ContractID(&c)
	if err != nil {
		return nodes.TaskRecord{}, nil, err
	}
	if sealed != rec.ContractHash {
		return nodes.TaskRecord{}, nil, fmt.Errorf("taskobj: task %q contract hashes to %s, record claims %s", key, sealed, rec.ContractHash)
	}
	return rec, &c, nil
}
