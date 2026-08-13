package agenttype

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/workmcp"
)

// TestShippedTierMatrices (orun-initiatives-v2 IS4): the shipped agent
// types carry the allow/ask/deny trust tiers, and the tiers keep their two
// promises. First: an sp_ seat cannot reach a signature by ANY composition
// of tools — the four signature tools (design_adopt, design_supersede,
// epic_approve, epic_revoke_approval) are never in an allow lane, so they
// cost a confirmation everywhere, an unattended session auto-denies the
// ask, and the cloud's model layer refuses non-human actors regardless
// (three layers, none trusted alone). Second: every work-plane-shaped name
// a policy speaks must exist on the workmcp roster — a typo'd entry is a
// silent deny-by-omission, which is exactly the shipped gap IS4 closes.
func TestShippedTierMatrices(t *testing.T) {
	t.Chdir(t.TempDir()) // force the embedded copies — test what ships

	signatures := []string{"design_adopt", "design_supersede", "epic_approve", "epic_revoke_approval"}
	roster := map[string]bool{}
	for _, name := range workmcp.ToolNames() {
		roster[name] = true
	}
	// Names with these prefixes belong to the work MCP's namespace; harness
	// tools (Read, Bash, …) are capitalized and catalog_*/connection_info
	// live with other providers.
	workPrefixes := []string{"work_", "task_", "spec_", "epic_", "milestone_",
		"design_", "initiative", "item_", "activity_", "contract_", "review_"}

	for _, typeName := range []string{"implementer", "orchestrator", "bootstrapper", "initiative-owner"} {
		d, issues := LoadNamed(typeName)
		if d == nil {
			t.Fatalf("%s: %v", typeName, issues)
		}
		for lane, names := range map[string][]string{"allow": d.Tools.Allow, "ask": d.Tools.Ask} {
			for _, name := range names {
				looksWork := false
				for _, p := range workPrefixes {
					if strings.HasPrefix(name, p) {
						looksWork = true
						break
					}
				}
				if looksWork && !roster[name] {
					t.Errorf("%s: %s lane names %q — not on the workmcp roster (typo = silent deny)", typeName, lane, name)
				}
			}
		}
		for _, sig := range signatures {
			for _, name := range d.Tools.Allow {
				if name == sig {
					t.Errorf("%s: signature tool %s in the allow lane — a signature must cost a confirmation", typeName, sig)
				}
			}
		}
		if len(d.Tools.Deny) == 0 || d.Tools.Deny[len(d.Tools.Deny)-1] != "*" {
			t.Errorf("%s: deny lane must backstop with %q (deny-by-default), got %v", typeName, "*", d.Tools.Deny)
		}
	}

	// The initiative-owner runs the FULL loop: primes from any key,
	// narrates, asserts, posts the headline, moves non-terminal state on
	// the allow lane — and may ASK for every signature.
	d, issues := LoadNamed("initiative-owner")
	if d == nil {
		t.Fatalf("initiative-owner: %v", issues)
	}
	allow := map[string]bool{}
	for _, n := range d.Tools.Allow {
		allow[n] = true
	}
	for _, n := range []string{"work_context", "work_now", "work_yours", "task_note", "task_done",
		"initiative_update_post", "initiative_status_set", "initiative_create",
		"design_propose", "review_request", "review_verdict", "item_assign"} {
		if !allow[n] {
			t.Errorf("initiative-owner: %s must be allow — the loop runs on it", n)
		}
	}
	ask := map[string]bool{}
	for _, n := range d.Tools.Ask {
		ask[n] = true
	}
	for _, sig := range signatures {
		if !ask[sig] {
			t.Errorf("initiative-owner: %s must ride the ask lane — the confirmation is the signature", sig)
		}
	}
}
