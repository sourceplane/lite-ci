package taskobj

// The O1 "done when", verbatim (orun-tasks implementation-plan): identical
// contracts dedup to one object, a revision produces a new hash, and a GC
// pass with a live task ref leaves its contract intact.

import (
	"context"
	"testing"

	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/contract"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objgc"
	"github.com/sourceplane/orun/internal/objindex"
	"github.com/sourceplane/orun/internal/objplan"
)

func rig(t *testing.T) (*objectstore.LocalStore, *refstore.LocalRefStore, *objindex.Indexer) {
	t.Helper()
	root := t.TempDir()
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Clock: clock.Fixed{}})
	if err != nil {
		t.Fatalf("refs: %v", err)
	}
	return store, refs, objindex.New(store, refs, root)
}

func fullContract() *contract.Contract {
	return &contract.Contract{
		Goal:     "ship the composer",
		Affects:  []string{"web-console-next"},
		DoneWhen: []string{"one row per change"},
		Gates:    []string{"tests"},
		Secrets:  []string{"STRIPE_TEST_*"},
		Envs:     []string{"dev"},
	}
}

func TestSealReadRoundtrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, refs, _ := rig(t)

	id, hash, err := SealTask(ctx, store, refs, SealInput{
		Key: "ENG-42", TaskRef: "tsk_3KF9TQ2P", Title: "Ship it", Contract: fullContract(),
	})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if hash == "" {
		t.Fatal("no contract hash sealed")
	}

	rec, c, err := ReadTask(ctx, store, refs, "ENG-42")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if rec.Key != "ENG-42" || rec.TaskRef != "tsk_3KF9TQ2P" || rec.ContractHash != hash {
		t.Fatalf("record roundtrip: %+v", rec)
	}
	if c == nil || c.Goal != "ship the composer" || len(c.Secrets) != 1 {
		t.Fatalf("contract roundtrip: %+v", c)
	}

	// Idempotent: an identical re-seal moves nothing.
	id2, _, err := SealTask(ctx, store, refs, SealInput{
		Key: "ENG-42", TaskRef: "tsk_3KF9TQ2P", Title: "Ship it", Contract: fullContract(),
	})
	if err != nil {
		t.Fatalf("re-seal: %v", err)
	}
	if id2 != id {
		t.Fatalf("identical seal moved the node: %s -> %s", id, id2)
	}
}

func TestIdenticalContractsDedupToOneObject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, refs, _ := rig(t)

	// Two DIFFERENT tasks carrying byte-identical contracts: the contract
	// blob is one object in the store — content addressing is the dedup.
	if _, _, err := SealTask(ctx, store, refs, SealInput{Key: "ENG-1", Contract: fullContract()}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := SealTask(ctx, store, refs, SealInput{Key: "WEB-2", Contract: fullContract()}); err != nil {
		t.Fatal(err)
	}

	_, wire, err := contract.ContractID(fullContract())
	if err != nil {
		t.Fatal(err)
	}
	blobID, err := objectstore.HashBlob(objectstore.AlgoSHA256, wire)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	if err := store.Iterate(ctx, func(id objectstore.ObjectID) error {
		if id == blobID {
			seen++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if seen != 1 {
		t.Fatalf("contract blob present %d times", seen)
	}
}

func TestRevisionMintsNewHashAndMovesRef(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, refs, _ := rig(t)

	id1, hash1, err := SealTask(ctx, store, refs, SealInput{Key: "ENG-42", Contract: fullContract()})
	if err != nil {
		t.Fatal(err)
	}
	revised := fullContract()
	revised.Envs = []string{"dev", "staging"}
	id2, hash2, err := SealTask(ctx, store, refs, SealInput{Key: "ENG-42", Contract: revised})
	if err != nil {
		t.Fatal(err)
	}
	if hash2 == hash1 || id2 == id1 {
		t.Fatalf("revision kept identity: node %s→%s hash %s→%s", id1, id2, hash1, hash2)
	}
	ref, err := refs.Read(ctx, objplan.TaskRefs("ENG-42"))
	if err != nil {
		t.Fatal(err)
	}
	if ref.Target != string(id2) {
		t.Fatalf("ref points at %s, want the revision %s", ref.Target, id2)
	}
}

func TestGCKeepsContractUnderLiveRefSweepsWithout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, refs, ix := rig(t)

	_, _, err := SealTask(ctx, store, refs, SealInput{Key: "ENG-42", Contract: fullContract()})
	if err != nil {
		t.Fatal(err)
	}
	_, wire, err := contract.ContractID(fullContract())
	if err != nil {
		t.Fatal(err)
	}
	blobID, err := objectstore.HashBlob(objectstore.AlgoSHA256, wire)
	if err != nil {
		t.Fatal(err)
	}

	// A GC pass with the task ref live leaves the contract intact — the ref
	// roots the tree, the tree bundles the contract.
	if _, err := objgc.Collect(ctx, store, refs, ix, objgc.Options{}); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if ok, _ := store.Has(ctx, blobID); !ok {
		t.Fatal("live task ref did not keep its contract")
	}

	// Delete the ref: the whole task closure is garbage on the next pass —
	// droppable by construction, like every projection in this system.
	if err := refs.Delete(ctx, objplan.TaskRefs("ENG-42")); err != nil {
		t.Fatal(err)
	}
	if _, err := objgc.Collect(ctx, store, refs, ix, objgc.Options{}); err != nil {
		t.Fatalf("collect after delete: %v", err)
	}
	if ok, _ := store.Has(ctx, blobID); ok {
		t.Fatal("unrooted contract survived the sweep")
	}
}

func TestReadRefusesTamperedContract(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, refs, _ := rig(t)

	if _, _, err := SealTask(ctx, store, refs, SealInput{Key: "ENG-42", Contract: fullContract()}); err != nil {
		t.Fatal(err)
	}
	// Re-point the ref at a hand-built tree whose contract bytes disagree
	// with the record's seal: the read must refuse, not reconcile.
	rec, _, err := ReadTask(ctx, store, refs, "ENG-42")
	if err != nil {
		t.Fatal(err)
	}
	forged := []byte(`{"goal":"forged"}`)
	forgedID, err := store.PutBlob(ctx, forged)
	if err != nil {
		t.Fatal(err)
	}
	recBytes, err := store.GetTree(ctx, mustRefTarget(t, refs, "ENG-42"))
	if err != nil {
		t.Fatal(err)
	}
	var taskEntry objectstore.TreeEntry
	for _, e := range recBytes {
		if e.Name == "task.json" {
			taskEntry = e
		}
	}
	tampered, err := store.PutTree(ctx, []objectstore.TreeEntry{
		taskEntry,
		{Name: "contract.json", Kind: objectstore.KindBlob, ID: forgedID},
	})
	if err != nil {
		t.Fatal(err)
	}
	cur, err := refs.Read(ctx, objplan.TaskRefs("ENG-42"))
	if err != nil {
		t.Fatal(err)
	}
	if err := refs.Update(ctx, objplan.TaskRefs("ENG-42"), cur.Target, string(tampered)); err != nil {
		t.Fatal(err)
	}

	if _, _, err := ReadTask(ctx, store, refs, "ENG-42"); err == nil {
		t.Fatalf("tampered contract read as %s without refusal", rec.ContractHash)
	}
}

func mustRefTarget(t *testing.T, refs *refstore.LocalRefStore, key string) objectstore.ObjectID {
	t.Helper()
	ref, err := refs.Read(context.Background(), objplan.TaskRefs(key))
	if err != nil {
		t.Fatal(err)
	}
	return objectstore.ObjectID(ref.Target)
}
