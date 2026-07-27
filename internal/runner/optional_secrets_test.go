package runner

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

// An optional (best-effort) reference whose key the resolver cannot supply is
// SKIPPED — no env var, no failure — while a missing REQUIRED reference still
// fails the job fail-closed. The wire-now-seed-later contract.
func TestOptionalSecretRefSkipsWhenUnresolved(t *testing.T) {
	exec := &envCapturingExecutor{echo: "ok"}
	r := newSecretTestRunner(exec)
	r.Hooks = &RunnerHooks{ResolveJobSecrets: func(_ string, refs []model.PlanSecretRef) (map[string]string, error) {
		// The resolver returns only the hard key; the optional one is absent
		// (not seeded yet).
		return map[string]string{"DATABASE_URL": "resolved-db-url"}, nil
	}}

	plan := secretPlan()
	plan.Jobs[0].SecretRefs = append(plan.Jobs[0].SecretRefs, model.PlanSecretRef{
		AsEnv:    "GITHUB_OAUTH_CLIENT_SECRET",
		Ref:      "secret://acme/api/prod/GITHUB_OAUTH_CLIENT_SECRET",
		Optional: true,
	})

	if err := r.Run(plan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	env := exec.envs["api@deploy/deploy"]
	if env == nil {
		t.Fatal("step env not captured")
	}
	if env["DATABASE_URL"] != "resolved-db-url" {
		t.Fatalf("hard ref not injected: %q", env["DATABASE_URL"])
	}
	if _, present := env["GITHUB_OAUTH_CLIENT_SECRET"]; present {
		t.Fatal("unresolved optional ref must not inject an env var")
	}
}

func TestRequiredSecretRefStillFailsClosed(t *testing.T) {
	exec := &envCapturingExecutor{echo: "ok"}
	r := newSecretTestRunner(exec)
	r.Hooks = &RunnerHooks{ResolveJobSecrets: func(_ string, _ []model.PlanSecretRef) (map[string]string, error) {
		return map[string]string{}, nil // nothing resolves
	}}

	err := r.Run(secretPlan())
	if err == nil {
		t.Fatal("expected the missing required reference to fail the run")
	}
	// The per-key message lands in the job state (the run error is the
	// fail-fast wrapper); the contract asserted here is fail-closed vs the
	// optional test's skip.
	if !strings.Contains(err.Error(), "failed") {
		t.Fatalf("expected a failed run, got: %v", err)
	}
}
