package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/sourcectx"
)

// TestAutopushEnabled covers the OR of the two sources. The user-config branch
// is exercised via HOME isolation (no file → off); the intent branch is direct.
func TestAutopushEnabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no ~/.orun/config.yaml → cloud.catalog.autopush off

	if autopushEnabled(nil) {
		t.Fatal("autopushEnabled(nil) = true, want false (no config)")
	}
	off := &model.Intent{}
	if autopushEnabled(off) {
		t.Fatal("autopushEnabled(off) = true, want false")
	}
	on := &model.Intent{}
	on.Execution.State.AutopushCatalog = true
	if !autopushEnabled(on) {
		t.Fatal("autopushEnabled(intent autopushCatalog=true) = false, want true")
	}
}

// TestCatalogAutoPublishScope pins the gate: the clean default branch is
// eligible for auto-publish — including when its head is ALSO tagged (a
// release workflow that tags every main head must not silently disable
// auto-sync; hit live). A feature branch / dirty tree / PR / detached tag
// never moves the project-wide head.
func TestCatalogAutoPublishScope(t *testing.T) {
	mainClean := sourcectx.WorkspaceState{Branch: "main", HeadRevision: "abc"}
	if !catalogAutoPublishScope(mainClean) {
		t.Fatal("branch-main must be auto-publishable")
	}
	taggedMain := sourcectx.WorkspaceState{Branch: "main", HeadRevision: "abc", Tag: "v1.2.3"}
	if !catalogAutoPublishScope(taggedMain) {
		t.Fatal("a TAGGED clean main head must be auto-publishable")
	}
	for name, ws := range map[string]sourcectx.WorkspaceState{
		"feature branch": {Branch: "feat", HeadRevision: "abc"},
		"dirty main":     {Branch: "main", HeadRevision: "abc", Dirty: true},
		"dirty tagged":   {Branch: "main", HeadRevision: "abc", Tag: "v1", Dirty: true},
		"detached tag":   {HeadRevision: "abc", Tag: "v1"},
		"pr":             {Branch: "main", HeadRevision: "abc", PRNumber: 7},
		"no git":         {},
	} {
		if catalogAutoPublishScope(ws) {
			t.Fatalf("%s must not be auto-publishable", name)
		}
	}
}

func TestAutopushMarkerRoundTrip(t *testing.T) {
	root := t.TempDir()
	if got := readAutopushMarker(root); got != "" {
		t.Fatalf("readAutopushMarker(empty) = %q, want \"\"", got)
	}
	const digest = "sha256:deadbeef"
	writeAutopushMarker(root, digest)
	if got := readAutopushMarker(root); got != digest {
		t.Fatalf("readAutopushMarker = %q, want %q", got, digest)
	}
}

// TestLoadIntentForCloudConfigReadsAutopushCatalog verifies the new config field
// parses from execution.state.
func TestLoadIntentForCloudConfigReadsAutopushCatalog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "intent.yaml")
	yaml := "apiVersion: orun/v1\nkind: Intent\nmetadata:\n  name: t\n" +
		"execution:\n  state:\n    backendUrl: https://example.test\n    autopushCatalog: true\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	prev := intentFile
	intentFile = path
	t.Cleanup(func() { intentFile = prev })

	intent := loadIntentForCloudConfig()
	if intent == nil {
		t.Fatal("loadIntentForCloudConfig() = nil")
	}
	if !intent.Execution.State.AutopushCatalog {
		t.Fatal("execution.state.autopushCatalog did not parse as true")
	}
}

// TestMaybeAutoPushCatalog_DisabledIsNoOp asserts auto-sync is inert when the
// config flag is off — it must not touch the network, write a marker, or panic.
func TestMaybeAutoPushCatalog_DisabledIsNoOp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(backendURLEnvVar, "")

	dir := t.TempDir()
	path := filepath.Join(dir, "intent.yaml")
	// autopushCatalog absent → defaults false.
	yaml := "apiVersion: orun/v1\nkind: Intent\nmetadata:\n  name: t\n" +
		"execution:\n  state:\n    backendUrl: https://example.test\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	prev := intentFile
	intentFile = path
	t.Cleanup(func() { intentFile = prev })

	// Must return promptly without error/panic.
	maybeAutoPushCatalog(context.Background())
}
