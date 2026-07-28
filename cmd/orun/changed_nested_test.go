package main

// changed_nested_test.go locks --changed attribution when component paths NEST:
// a file under a nested component's path must scope in the nested component by
// longest-prefix, never silently fall through to the enclosing component. The
// incident shape: `db` owns packages/db, `db-migrate` owns
// packages/db/src/migrations; a new migration file selected only `db`, so the
// migration lane never ran. Each test authors the same two components in a
// different supported form — inline intent components, nested component.yaml
// files, and spec.path pointing away from the component.yaml's own dir (the
// layout that regressed: ownership used to index only the SourceFile dir).

import (
	"os"
	"path/filepath"
	"testing"
)

const nestedMigrationFile = "packages/db/src/migrations/990_add_shipping/up.sql"

func writeWorkspace(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// expectSelection asserts the engine's --changed selection for files equals want.
func expectSelection(t *testing.T, root string, files, want []string) {
	t.Helper()
	got := engineSelection(t, root, files, "watch")
	if !equalStringSets(got, sliceToSet(want)) {
		t.Errorf("selection for %v = %v, want %v", files, sortedSet(got), want)
	}
}

func TestChangedNestedPaths_InlineComponents(t *testing.T) {
	root := writeWorkspace(t, map[string]string{
		"intent.yaml": `apiVersion: orun.io/v1alpha1
kind: Intent
metadata:
  name: nested
catalog:
  namespace: ns
components:
  - name: db
    type: library
    path: packages/db
  - name: db-migrate
    type: job
    path: packages/db/src/migrations
`,
		"packages/db/index.ts": "export {}\n",
		nestedMigrationFile:    "create table shipping(id int);\n",
	})
	// The migration is under BOTH paths; longest-prefix scopes the nested
	// component, not the enclosing one.
	expectSelection(t, root, []string{nestedMigrationFile}, []string{"db-migrate"})
	// A file under packages/db outside the nested subtree still maps to db.
	expectSelection(t, root, []string{"packages/db/index.ts"}, []string{"db"})
	expectSelection(t, root, []string{"packages/db/index.ts", nestedMigrationFile}, []string{"db", "db-migrate"})
}

func TestChangedNestedPaths_NestedComponentYAML(t *testing.T) {
	root := writeWorkspace(t, map[string]string{
		"intent.yaml": `apiVersion: orun.io/v1alpha1
kind: Intent
metadata:
  name: nested
catalog:
  namespace: ns
`,
		"packages/db/component.yaml": `apiVersion: orun.io/v1alpha1
kind: Component
metadata:
  name: db
spec:
  type: library
`,
		"packages/db/src/migrations/component.yaml": `apiVersion: orun.io/v1alpha1
kind: Component
metadata:
  name: db-migrate
spec:
  type: job
`,
		"packages/db/index.ts": "export {}\n",
		nestedMigrationFile:    "create table shipping(id int);\n",
	})
	expectSelection(t, root, []string{nestedMigrationFile}, []string{"db-migrate"})
	expectSelection(t, root, []string{"packages/db/index.ts"}, []string{"db"})
}

// TestChangedNestedPaths_SpecPathAuthored is the incident regression: the
// nested component's component.yaml lives OUTSIDE the tree it owns and names
// it via spec.path. Ownership must index the authored path (not just the
// component.yaml's dir) or the migration attributes to db alone.
func TestChangedNestedPaths_SpecPathAuthored(t *testing.T) {
	root := writeWorkspace(t, map[string]string{
		"intent.yaml": `apiVersion: orun.io/v1alpha1
kind: Intent
metadata:
  name: nested
catalog:
  namespace: ns
`,
		"packages/db/component.yaml": `apiVersion: orun.io/v1alpha1
kind: Component
metadata:
  name: db
spec:
  type: library
  path: packages/db
`,
		"tools/db-migrate/component.yaml": `apiVersion: orun.io/v1alpha1
kind: Component
metadata:
  name: db-migrate
spec:
  type: job
  path: packages/db/src/migrations
`,
		"packages/db/index.ts":       "export {}\n",
		"tools/db-migrate/runner.sh": "#!/bin/sh\n",
		nestedMigrationFile:          "create table shipping(id int);\n",
	})
	expectSelection(t, root, []string{nestedMigrationFile}, []string{"db-migrate"})
	expectSelection(t, root, []string{"packages/db/index.ts"}, []string{"db"})
	// The component.yaml's own dir still belongs to db-migrate too.
	expectSelection(t, root, []string{"tools/db-migrate/runner.sh"}, []string{"db-migrate"})
}
