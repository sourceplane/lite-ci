package objplan

import (
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogresolve"
)

// pathView builds a view whose manifests exercise the spec.path ownership
// entries: a component authored away from the tree it owns must claim its
// authored dir too, or files under it mis-attribute to an enclosing
// component by longest-prefix (the nested-migrations incident).
func pathView(manifests ...*catalogmodel.ComponentManifest) *catalogresolve.CatalogView {
	return &catalogresolve.CatalogView{
		ResolvedCatalog: &catalogresolve.ResolvedCatalog{Manifests: manifests},
		Snapshot:        &catalogmodel.CatalogSnapshot{},
	}
}

func manifest(key, sourceFile, specPath string) *catalogmodel.ComponentManifest {
	return &catalogmodel.ComponentManifest{
		Identity: catalogmodel.ComponentIdentity{
			ComponentKey: key,
			SourceFile:   sourceFile,
			Path:         specPath,
		},
	}
}

func TestBuildOwnership_AuthoredPathClaimsNestedDir(t *testing.T) {
	t.Parallel()
	view := pathView(
		manifest("ns/repo/db", "packages/db/component.yaml", "packages/db"),
		manifest("ns/repo/db-migrate", "tools/db-migrate/component.yaml", "packages/db/src/migrations"),
	)
	o := buildOwnership(view)
	want := map[string]string{
		"packages/db":                "ns/repo/db",
		"tools/db-migrate":           "ns/repo/db-migrate",
		"packages/db/src/migrations": "ns/repo/db-migrate",
	}
	if len(o.Components) != len(want) {
		t.Fatalf("components = %v, want %v", o.Components, want)
	}
	for dir, key := range want {
		if got := o.Components[dir]; got != key {
			t.Errorf("components[%q] = %q, want %q", dir, got, key)
		}
	}
	if err := o.Validate(); err != nil {
		t.Fatalf("ownership invalid: %v", err)
	}
}

func TestBuildOwnership_DirResidentClaimWinsOverAuthoredPath(t *testing.T) {
	t.Parallel()
	// intruder authors spec.path pointing at db's own dir: the dir-resident
	// component.yaml keeps the claim.
	view := pathView(
		manifest("ns/repo/db", "packages/db/component.yaml", ""),
		manifest("ns/repo/intruder", "tools/intruder/component.yaml", "packages/db"),
	)
	o := buildOwnership(view)
	if got := o.Components["packages/db"]; got != "ns/repo/db" {
		t.Errorf("dir-resident claim lost: components[packages/db] = %q", got)
	}
	if got := o.Components["tools/intruder"]; got != "ns/repo/intruder" {
		t.Errorf("intruder's own dir = %q, want ns/repo/intruder", got)
	}
}

func TestBuildOwnership_AuthoredPathTieIsDeterministic(t *testing.T) {
	t.Parallel()
	// Two components author the same spec.path: the first in manifest order
	// (componentKey-sorted) wins, on every build.
	view := pathView(
		manifest("ns/repo/a", "tools/a/component.yaml", "shared/dir"),
		manifest("ns/repo/b", "tools/b/component.yaml", "shared/dir"),
	)
	for i := 0; i < 10; i++ {
		o := buildOwnership(view)
		if got := o.Components["shared/dir"]; got != "ns/repo/a" {
			t.Fatalf("run %d: components[shared/dir] = %q, want ns/repo/a", i, got)
		}
	}
}

func TestBuildOwnership_UnusablePathsSkipped(t *testing.T) {
	t.Parallel()
	view := pathView(
		manifest("ns/repo/abs", "tools/abs/component.yaml", "/etc/passwd"),
		manifest("ns/repo/esc", "tools/esc/component.yaml", "../outside"),
	)
	o := buildOwnership(view)
	want := map[string]string{
		"tools/abs": "ns/repo/abs",
		"tools/esc": "ns/repo/esc",
	}
	if len(o.Components) != len(want) {
		t.Fatalf("unusable spec.path leaked into ownership: %v", o.Components)
	}
	if err := o.Validate(); err != nil {
		t.Fatalf("ownership invalid: %v", err)
	}
}

func TestOwnershipDirFromPath(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"packages/db":                "packages/db",
		"packages/db/":               "packages/db",
		"./packages/db":              "packages/db",
		"packages//db":               "packages/db",
		"packages\\db":               "packages/db",
		" packages/db ":              "packages/db",
		".":                          ".",
		"":                           "",
		"/abs/path":                  "",
		"..":                         "",
		"../up":                      "",
		"a/../../up":                 "",
		"packages/db/src/migrations": "packages/db/src/migrations",
	}
	for in, want := range cases {
		if got := ownershipDirFromPath(in); got != want {
			t.Errorf("ownershipDirFromPath(%q) = %q, want %q", in, got, want)
		}
	}
}
