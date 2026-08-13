// Package platformmcp is the platform tool plane of the orun MCP
// (orun-mcp UM1+UM2): the tools of the shipped TS plane
// (orun-cloud/packages/mcp), reimplemented natively over the public API via
// internal/remotestate. The stdio transport lives in internal/mcpserve; this
// package supplies the tools.
//
// The vendored manifest carries 29 tools (23 reads + 6 writes); this plane
// advertises 25 of them (19 reads + 6 writes) because four work-plane reads
// are ceded to internal/workmcp — see cededToWorkPlane.
//
// Tool names, descriptions, input schemas, and annotations are EMBEDDED from
// the vendored contract (specs/orun-cloud/vendored/mcp-tool-manifest.json —
// design §4: the TS plane is the source of truth). mcp-tool-manifest.json in
// this directory is a byte-for-byte copy for go:embed (embed cannot reach
// outside the package dir); parity_test.go pins the copy to the vendored file
// and the advertised roster to the manifest, so schema parity holds by
// construction and drift is a test failure.
package platformmcp

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed mcp-tool-manifest.json
var manifestJSON []byte

// manifestTool is one tool entry of the vendored manifest.
type manifestTool struct {
	Name        string                 `json:"name"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	InputSchema json.RawMessage        `json:"inputSchema"`
	Annotations map[string]interface{} `json:"annotations"`
}

// manifestResource / manifestPrompt are the manifest's reserved MCP9 stubs,
// consumed since UM6: the 2 resource templates and 4 prompts the TS plane
// registers. The stub is minimal — no mimeType (the provider supplies
// text/markdown) and no prompt text (rendering is implementation-side; the
// texts are ported from the TS plane's prompts.ts and drift-guarded in tests).
type manifestResource struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	URITemplate string `json:"uriTemplate"`
}

type manifestPromptArg struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

type manifestPrompt struct {
	Name        string              `json:"name"`
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Args        []manifestPromptArg `json:"args"`
}

type manifest struct {
	ManifestVersion   int                `json:"manifestVersion"`
	ReadOnlyToolCount int                `json:"readOnlyToolCount"`
	ToolCount         int                `json:"toolCount"`
	Tools             []manifestTool     `json:"tools"`
	Resources         []manifestResource `json:"resources"`
	Prompts           []manifestPrompt   `json:"prompts"`
}

// cededToWorkPlane names the vendored tools this plane does NOT advertise
// because internal/workmcp already serves them natively over local work
// state. The TS plane added them (orun-cloud IN9) for hosted agents reaching
// the remote MCP, which have no orun binary and so no work plane at all;
// here the work provider answers them from the fold directly.
//
// Ceding is not merely cosmetic. mcpserve.checkRoster rejects a roster
// carrying one name twice, so without this filter `orun mcp serve` fails at
// startup the moment both planes mount. And the platform provider has no
// implementation for these four — p.call would fall through to "unknown
// tool" — so advertising them would promise a tool this plane cannot serve.
//
// A name that leaves the TS manifest leaves this set stale but harmless;
// parity_test.go asserts every entry still resolves, so the staleness is a
// test failure rather than an init-time panic that would brick the binary.
var cededToWorkPlane = map[string]bool{
	"initiatives_list": true,
	"initiative_tree":  true,
	"task_get":         true,
	"activity_get":     true,
}

// toolSpec is one advertised tool: the manifest entry plus what the dispatch
// layer needs (workspace-argument shape for the default fill, read/write for
// the --read-only filter).
type toolSpec struct {
	manifestTool
	readOnly          bool
	hasWorkspace      bool
	workspaceRequired bool
}

// embedded is the parsed manifest; allTools the plane's advertised roster
// (19 reads + 6 writes — the manifest's 29 less the 4 ceded to the work
// plane), in manifest order. Resources and prompts come off embedded
// directly (resources.go / prompts.go).
var embedded = mustParseManifest()

var allTools = mustLoadTools(embedded)

var toolsByName = func() map[string]*toolSpec {
	m := make(map[string]*toolSpec, len(allTools))
	for i := range allTools {
		m[allTools[i].Name] = &allTools[i]
	}
	return m
}()

func mustParseManifest() manifest {
	var m manifest
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		panic(fmt.Sprintf("platformmcp: embedded manifest is invalid: %v", err))
	}
	return m
}

func mustLoadTools(m manifest) []toolSpec {
	reads, ceded := 0, 0
	out := make([]toolSpec, 0, len(m.Tools))
	for _, t := range m.Tools {
		spec := toolSpec{manifestTool: t}
		spec.readOnly, _ = t.Annotations["readOnlyHint"].(bool)
		if spec.readOnly {
			reads++
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		}
		if err := json.Unmarshal(t.InputSchema, &schema); err != nil {
			panic(fmt.Sprintf("platformmcp: tool %s has an invalid inputSchema: %v", t.Name, err))
		}
		_, spec.hasWorkspace = schema.Properties["workspace"]
		for _, r := range schema.Required {
			if r == "workspace" {
				spec.workspaceRequired = true
			}
		}
		if cededToWorkPlane[t.Name] {
			ceded++
			continue
		}
		out = append(out, spec)
	}
	// The manifest's own integrity is checked over the full roster — ceding
	// removes tools from what this plane advertises, never from the contract
	// it was handed.
	if len(out)+ceded != m.ToolCount || reads != m.ReadOnlyToolCount {
		panic(fmt.Sprintf("platformmcp: embedded manifest advertises %d tools (%d read-only) but carries %d (%d read-only)",
			m.ToolCount, m.ReadOnlyToolCount, len(out)+ceded, reads))
	}
	return out
}

// decodeSchema re-unmarshals a tool's schema into a fresh mutable map (the
// advertised copy may be adjusted per provider; the manifest form never is).
func decodeSchema(raw json.RawMessage) map[string]interface{} {
	var schema map[string]interface{}
	if err := json.Unmarshal(raw, &schema); err != nil {
		panic(fmt.Sprintf("platformmcp: schema decode: %v", err))
	}
	return schema
}

// dropRequired removes name from the schema's required array (deleting the
// key when it empties) — MCP1's applyWorkspaceDefault semantics: an active
// ambient default makes `workspace` optional on the advertised schema.
func dropRequired(schema map[string]interface{}, name string) {
	req, ok := schema["required"].([]interface{})
	if !ok {
		return
	}
	out := make([]interface{}, 0, len(req))
	for _, r := range req {
		if s, ok := r.(string); ok && s == name {
			continue
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		delete(schema, "required")
	} else {
		schema["required"] = out
	}
}
