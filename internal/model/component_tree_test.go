package model

import "testing"

// The discovery cache round-trip must preserve every authored field of a
// component manifest. SecretEnv regressed here once: SEC0 added it to
// Component but not to ComponentTreeComponent, so a component.yaml declaring
// spec.secretEnv lost its references before expansion — jobs planned with no
// secretRefs and the leak was silent (no compile error, no warning).
func TestComponentTreeRoundTripPreservesSecretEnv(t *testing.T) {
	component := Component{
		Name:    "cloudflare-kv",
		Type:    "terraform",
		Enabled: true,
		Env:     map[string]string{"AWS_EC2_METADATA_DISABLED": "true"},
		SecretEnv: map[string]string{
			"CLOUDFLARE_API_TOKEN": "secret://acme/api/{{ .environment }}/CLOUDFLARE_API_TOKEN",
		},
	}

	entry := FromComponent(component, "test")
	if got := entry.SecretEnv["CLOUDFLARE_API_TOKEN"]; got != component.SecretEnv["CLOUDFLARE_API_TOKEN"] {
		t.Fatalf("FromComponent dropped SecretEnv: got %q", got)
	}

	back := entry.ToComponent()
	if got := back.SecretEnv["CLOUDFLARE_API_TOKEN"]; got != component.SecretEnv["CLOUDFLARE_API_TOKEN"] {
		t.Fatalf("ToComponent dropped SecretEnv: got %q", got)
	}
}
