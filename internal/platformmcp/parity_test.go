package platformmcp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/mcpserve"
)

// The anti-drift contract (design §4): the vendored manifest exported from
// the TS plane pins this package's roster. These tests fail on any name /
// description / schema / annotation drift — over the whole advertised roster
// since UM2 (reads and writes alike).
//
// The manifest carries 27 tools and this plane advertises all of them. It
// advertised a subset until WT2, when the four work-plane reads it used to
// cede left the TS manifest with the plane that served them.

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root (go.mod)")
		}
		dir = parent
	}
}

func vendoredManifest(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join(repoRoot(t), "specs", "orun-cloud", "vendored", "mcp-tool-manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read vendored manifest: %v", err)
	}
	return data
}

// TestEmbeddedManifestMatchesVendored pins the go:embed copy in this package
// to the vendored contract byte-for-byte (embed cannot reach specs/ from
// here; the copy must be re-copied on every re-vendor).
func TestEmbeddedManifestMatchesVendored(t *testing.T) {
	if !bytes.Equal(vendoredManifest(t), manifestJSON) {
		t.Fatal("internal/platformmcp/mcp-tool-manifest.json differs from " +
			"specs/orun-cloud/vendored/mcp-tool-manifest.json; re-copy the vendored file " +
			"into the package (cp specs/orun-cloud/vendored/mcp-tool-manifest.json internal/platformmcp/)")
	}
}

// TestVendoredManifestChecksum is the drift guard for the vendored tool
// manifest, mirroring TestVendoredContractChecksum (OC0 pattern): the
// vendored file must match the sha256 recorded in the sibling CHECKSUM.
func TestVendoredManifestChecksum(t *testing.T) {
	sum := sha256.Sum256(vendoredManifest(t))
	got := hex.EncodeToString(sum[:])

	f, err := os.Open(filepath.Join(repoRoot(t), "specs", "orun-cloud", "vendored", "CHECKSUM"))
	if err != nil {
		t.Fatalf("open CHECKSUM: %v", err)
	}
	defer f.Close()
	want := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "mcp-tool-manifest.json" {
			want = fields[0]
		}
	}
	if want == "" {
		t.Fatal("specs/orun-cloud/vendored/CHECKSUM has no entry for mcp-tool-manifest.json")
	}
	if got != want {
		t.Fatalf("vendored mcp-tool-manifest.json drifted from its recorded checksum:\n"+
			"  recorded (CHECKSUM): %s\n  actual   (file):     %s\n"+
			"re-vendor from orun-cloud (MCP9 export) then update CHECKSUM and the embedded copy.", want, got)
	}
}

// canon renders a decoded JSON value canonically (json.Marshal sorts object
// keys), normalizing both sides of a schema comparison the same way.
func canon(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("canon: %v", err)
	}
	return string(b)
}

// TestManifestParity asserts the advertised roster equals the vendored
// manifest's unceded tools, tool for tool: count, order, names,
// descriptions, titles, annotations, and normalized inputSchemas — 27 of the
// manifest's 31, and the readOnlyHint:true 21 of those under ReadOnly.
func TestManifestParity(t *testing.T) {
	var m manifest
	if err := json.Unmarshal(vendoredManifest(t), &m); err != nil {
		t.Fatalf("parse vendored manifest: %v", err)
	}
	if m.ToolCount != len(m.Tools) {
		t.Fatalf("manifest toolCount %d != %d tools", m.ToolCount, len(m.Tools))
	}
	if len(m.Tools) != 27 {
		t.Fatalf("WT2 expects 27 tools in the manifest, it carries %d", len(m.Tools))
	}

	var manifestReads int
	var wantAll, wantReads []manifestTool
	for _, tool := range m.Tools {
		ro, _ := tool.Annotations["readOnlyHint"].(bool)
		if ro {
			manifestReads++
		}
		wantAll = append(wantAll, tool)
		if ro {
			wantReads = append(wantReads, tool)
		}
	}
	if manifestReads != m.ReadOnlyToolCount {
		t.Fatalf("manifest readOnlyToolCount %d but %d readOnlyHint:true tools", m.ReadOnlyToolCount, manifestReads)
	}
	if manifestReads != 21 {
		t.Fatalf("WT2 expects 21 read tools in the manifest, it carries %d", manifestReads)
	}
	if len(wantAll) != 27 {
		t.Fatalf("WT2 expects 27 advertised tools, got %d", len(wantAll))
	}
	if len(wantReads) != 21 {
		t.Fatalf("WT2 expects 21 advertised read tools, got %d", len(wantReads))
	}

	for _, tc := range []struct {
		mode string
		got  []mcpserve.ToolDef
		want []manifestTool
	}{
		// no default workspace: schemas must be the manifest's, verbatim
		{"full", (&Provider{}).Tools(), wantAll},
		{"read-only", (&Provider{ReadOnly: true}).Tools(), wantReads},
	} {
		if len(tc.got) != len(tc.want) {
			t.Fatalf("%s: Provider.Tools() = %d tools, want %d", tc.mode, len(tc.got), len(tc.want))
		}
		for i, w := range tc.want {
			g := tc.got[i]
			if g.Name != w.Name {
				t.Fatalf("%s tool %d: name %q, want %q (order must match the manifest)", tc.mode, i, g.Name, w.Name)
			}
			if g.Description != w.Description {
				t.Errorf("%s: description drifted from the manifest", w.Name)
			}
			if g.Title != w.Title {
				t.Errorf("%s: title = %q, want %q", w.Name, g.Title, w.Title)
			}
			if canon(t, g.Annotations) != canon(t, w.Annotations) {
				t.Errorf("%s: annotations = %s, want %s", w.Name, canon(t, g.Annotations), canon(t, w.Annotations))
			}
			var wantSchema interface{}
			if err := json.Unmarshal(w.InputSchema, &wantSchema); err != nil {
				t.Fatalf("%s: manifest schema: %v", w.Name, err)
			}
			if canon(t, g.InputSchema) != canon(t, wantSchema) {
				t.Errorf("%s: inputSchema drifted:\n got  %s\n want %s", w.Name, canon(t, g.InputSchema), canon(t, wantSchema))
			}
		}
	}
}

// TestNoWorkPlaneNamesSurvive: the four reads this plane used to cede to
// internal/workmcp — initiatives_list, initiative_tree, task_get,
// activity_get — left the TS manifest with the work plane at WT2. If a
// later re-vendor reintroduces a name no local plane serves, the ceding
// machinery is gone and the plane would advertise a tool it cannot answer,
// so the guard now runs the other way: those names must not come back
// without an implementation.
func TestNoWorkPlaneNamesSurvive(t *testing.T) {
	var m manifest
	if err := json.Unmarshal(vendoredManifest(t), &m); err != nil {
		t.Fatalf("parse vendored manifest: %v", err)
	}
	retired := map[string]bool{
		"initiatives_list": true, "initiative_tree": true,
		"task_get": true, "activity_get": true,
	}
	for _, tool := range m.Tools {
		if retired[tool.Name] {
			t.Errorf("vendored manifest carries %q — a retired work-plane tool; this plane has no implementation for it", tool.Name)
		}
	}
	p := &Provider{}
	for name := range retired {
		if _, owned := p.Call(context.Background(), name, nil); owned {
			t.Errorf("%s: Call claims ownership of a retired work-plane tool", name)
		}
	}
}

// TestResourcesPromptsParity (UM6): the advertised resource templates and
// prompts equal the vendored manifest's reserved sections — names, titles,
// descriptions, uriTemplates, and prompt args including required flags, in
// manifest order — and neither surface is filtered under ReadOnly.
func TestResourcesPromptsParity(t *testing.T) {
	var m manifest
	if err := json.Unmarshal(vendoredManifest(t), &m); err != nil {
		t.Fatalf("parse vendored manifest: %v", err)
	}
	if len(m.Resources) != 2 {
		t.Fatalf("UM6 expects 2 resource templates, manifest carries %d", len(m.Resources))
	}
	if len(m.Prompts) != 4 {
		t.Fatalf("UM6 expects 4 prompts, manifest carries %d", len(m.Prompts))
	}

	templates := (&Provider{}).ResourceTemplates()
	if len(templates) != len(m.Resources) {
		t.Fatalf("ResourceTemplates() = %d, want %d", len(templates), len(m.Resources))
	}
	for i, w := range m.Resources {
		g := templates[i]
		if g.Name != w.Name || g.Title != w.Title || g.Description != w.Description || g.URITemplate != w.URITemplate {
			t.Errorf("resource %d drifted from the manifest:\n got  %+v\n want %+v", i, g, w)
		}
		if g.MimeType != "text/markdown" {
			t.Errorf("%s: mimeType = %q, want text/markdown", w.Name, g.MimeType)
		}
	}

	prompts := (&Provider{}).Prompts()
	if len(prompts) != len(m.Prompts) {
		t.Fatalf("Prompts() = %d, want %d", len(prompts), len(m.Prompts))
	}
	for i, w := range m.Prompts {
		g := prompts[i]
		if g.Name != w.Name || g.Title != w.Title || g.Description != w.Description {
			t.Errorf("prompt %d drifted from the manifest:\n got  %s/%s\n want %s/%s", i, g.Name, g.Description, w.Name, w.Description)
		}
		if len(g.Arguments) != len(w.Args) {
			t.Errorf("%s: %d args, want %d", w.Name, len(g.Arguments), len(w.Args))
			continue
		}
		for j, wa := range w.Args {
			ga := g.Arguments[j]
			if ga.Name != wa.Name || ga.Description != wa.Description || ga.Required != wa.Required {
				t.Errorf("%s arg %d drifted:\n got  %+v\n want %+v", w.Name, j, ga, wa)
			}
		}
	}

	// Read-only never filters resources/prompts (they are read-only context /
	// text templates by construction — the TS posture).
	ro := &Provider{ReadOnly: true}
	if len(ro.ResourceTemplates()) != 2 || len(ro.Prompts()) != 4 {
		t.Fatalf("ReadOnly filtered resources/prompts: %d templates, %d prompts",
			len(ro.ResourceTemplates()), len(ro.Prompts()))
	}
}

// TestWorkspaceDefaultAdjustsSchema: with an ambient default, `workspace`
// drops out of each schema's required array (and only that — properties are
// untouched); tools without a workspace argument are unchanged.
func TestWorkspaceDefaultAdjustsSchema(t *testing.T) {
	tools := (&Provider{DefaultWorkspace: "ws_ambient"}).Tools()
	for _, tool := range tools {
		props, _ := tool.InputSchema["properties"].(map[string]interface{})
		_, hasWorkspace := props["workspace"]
		req, _ := tool.InputSchema["required"].([]interface{})
		for _, r := range req {
			if r == "workspace" {
				t.Errorf("%s: workspace still required despite an active default", tool.Name)
			}
		}
		if !hasWorkspace && len(req) == 0 {
			switch tool.Name {
			case "whoami", "workspaces_list", "security_events_list":
			default:
				t.Errorf("%s: unexpectedly has neither a workspace property nor required args", tool.Name)
			}
		}
	}
	// projects_list requires only workspace in the manifest; with the default
	// active its required key must vanish entirely.
	for _, tool := range tools {
		if tool.Name == "projects_list" {
			if _, ok := tool.InputSchema["required"]; ok {
				t.Errorf("projects_list: required = %v, want the key dropped", tool.InputSchema["required"])
			}
		}
		if tool.Name == "catalog_get_entity" {
			req, _ := tool.InputSchema["required"].([]interface{})
			if canon(t, req) != `["entityRef"]` {
				t.Errorf("catalog_get_entity: required = %s, want [\"entityRef\"]", canon(t, req))
			}
		}
	}
}
