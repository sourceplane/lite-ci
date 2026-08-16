package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sourceplane/orun/internal/nodes"
)

func TestWriteMCPConfigFiltersThroughPolicy(t *testing.T) {
	dir := t.TempDir()
	policy := NewToolPolicy(nodes.AgentToolPolicy{
		Allow: []string{"pr_open", "connection_info"},
		Ask:   []string{"webhook_create"},
		Deny:  []string{"*"},
	})
	tools := []string{"pr_open", "connection_info", "member_invite", "webhook_create"}
	setup, err := WriteMCPConfig(dir, policy, tools, nil)
	if err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(setup.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		MCPServers map[string]MCPServer `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	orun, ok := cfg.MCPServers["orun"]
	if !ok || orun.Command == "" || len(orun.Args) != 2 {
		t.Fatalf("orun MCP server entry = %+v", orun)
	}
	// PATH-proof: the command must be THIS process's absolute executable (the
	// harness spawns MCP servers with an inherited PATH that may not carry
	// ~/.local/bin in a cloud sandbox), falling back to "orun" only when the
	// executable cannot be resolved at all.
	if orun.Command != "orun" && !filepath.IsAbs(orun.Command) {
		t.Fatalf("orun MCP server command must be absolute (or the bare fallback), got %q", orun.Command)
	}

	wantAllow := []string{"mcp__orun__connection_info", "mcp__orun__pr_open"}
	if !reflect.DeepEqual(setup.Allowed, wantAllow) {
		t.Fatalf("allowed = %v", setup.Allowed)
	}
	// Denied tools are gated at the harness too; ask-gated tools are in
	// NEITHER list — the harness prompts and the prompt becomes an
	// approval_requested.
	wantDeny := []string{"mcp__orun__member_invite"}
	if !reflect.DeepEqual(setup.Disallowed, wantDeny) {
		t.Fatalf("disallowed = %v", setup.Disallowed)
	}

	args := setup.HarnessArgs()
	want := []string{"--allowedTools", "mcp__orun__connection_info,mcp__orun__pr_open",
		"--disallowedTools", "mcp__orun__member_invite"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("harness args = %v", args)
	}

	// The file is private: it may later carry remote-server headers.
	info, err := os.Stat(setup.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestWriteMCPConfigMergesExtraServers(t *testing.T) {
	dir := t.TempDir()
	setup, err := WriteMCPConfig(dir, ToolPolicy{}, nil, map[string]MCPServer{
		"platform": {URL: "https://mcp.example.com/v1", Type: "http",
			Headers: map[string]string{"Authorization": "Bearer tok"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(setup.ConfigPath)
	var cfg struct {
		MCPServers map[string]MCPServer `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.MCPServers["platform"].URL != "https://mcp.example.com/v1" {
		t.Fatalf("platform server missing: %+v", cfg.MCPServers)
	}
	if _, ok := cfg.MCPServers["orun"]; !ok {
		t.Fatal("orun server must always be present")
	}
}

func TestPolicyToolNameNormalization(t *testing.T) {
	// The runtime authority must speak the same names as its own config:
	// harness-reported mcp__orun__* maps back to the bare policy name.
	if got := policyToolName("mcp__orun__pr_open"); got != "pr_open" {
		t.Fatalf("got %q", got)
	}
	if got := policyToolName("Bash"); got != "Bash" {
		t.Fatalf("harness tools pass through, got %q", got)
	}
	p := NewToolPolicy(nodes.AgentToolPolicy{Allow: []string{"pr_open"}, Deny: []string{"*"}})
	if p.Decide(policyToolName("mcp__orun__pr_open")) != DecisionAllow {
		t.Fatal("an allowed MCP tool must not be denied by the authority")
	}
}
