package runner

// SEC-JOB producer half: the sink contract. Declared keys are an ALLOW-list
// (undeclared publish fails the job), plan-only lanes with an empty sink are
// a no-op, values are redacted before the publish hook sees them, and the
// heredoc form round-trips multiline values.

import (
	"os"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/redact"
)

func outputsJob(uid, spec string) model.PlanJob {
	return model.PlanJob{
		ID:          "cloudflare-kv.stage.terraform",
		UID:         uid,
		Component:   "cloudflare-kv",
		Environment: "stage",
		Parameters:  map[string]interface{}{"secretOutputs": spec},
	}
}

func TestJobSecretOutputKeysParsesKeyHalves(t *testing.T) {
	keys := jobSecretOutputKeys(outputsJob("u1", "WIRING_CLOUDFLARE_KV=wiring, SUPABASE_DB_URL=connection_uri"))
	if len(keys) != 2 || keys[0] != "WIRING_CLOUDFLARE_KV" || keys[1] != "SUPABASE_DB_URL" {
		t.Fatalf("keys = %v", keys)
	}
	if got := jobSecretOutputKeys(model.PlanJob{}); got != nil {
		t.Fatalf("undeclared job returned keys: %v", got)
	}
}

func TestPublishJobOutputSecretsRoundTrip(t *testing.T) {
	job := outputsJob("u-roundtrip", "WIRING_CLOUDFLARE_KV=wiring,SUPABASE_DB_URL=uri")
	r := &Runner{Stderr: os.Stderr, Redactor: redact.New()}
	published := map[string]string{}
	r.Hooks = &RunnerHooks{PublishJobOutputs: func(jobID string, secrets map[string]string) error {
		for k, v := range secrets {
			published[k] = v
		}
		return nil
	}}
	sink := r.jobSecretOutputsSinkPath(job)
	t.Cleanup(func() { _ = os.Remove(sink) })
	content := "WIRING_CLOUDFLARE_KV<<__ORUN_EOF__\n{\n  \"id\": \"abc\"\n}\n__ORUN_EOF__\nSUPABASE_DB_URL=postgres://u:p@h:5432/db\n"
	if err := os.WriteFile(sink, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.publishJobOutputSecrets(job); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if published["WIRING_CLOUDFLARE_KV"] != "{\n  \"id\": \"abc\"\n}" {
		t.Fatalf("heredoc value = %q", published["WIRING_CLOUDFLARE_KV"])
	}
	if published["SUPABASE_DB_URL"] != "postgres://u:p@h:5432/db" {
		t.Fatalf("kv value = %q", published["SUPABASE_DB_URL"])
	}
	// Values reached the redactor before the hook.
	if masked := r.Redactor.Filter("x postgres://u:p@h:5432/db y"); strings.Contains(masked, "postgres://u:p@h") {
		t.Fatalf("value not redacted: %q", masked)
	}
	if _, err := os.Stat(sink); !os.IsNotExist(err) {
		t.Fatal("sink not cleaned up")
	}
}

func TestPublishJobOutputSecretsAllowList(t *testing.T) {
	job := outputsJob("u-allow", "DECLARED=x")
	r := &Runner{Stderr: os.Stderr}
	r.Hooks = &RunnerHooks{PublishJobOutputs: func(string, map[string]string) error {
		t.Fatal("hook must not be called for undeclared keys")
		return nil
	}}
	sink := r.jobSecretOutputsSinkPath(job)
	t.Cleanup(func() { _ = os.Remove(sink) })
	if err := os.WriteFile(sink, []byte("SNEAKY=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := r.publishJobOutputSecrets(job)
	if err == nil || !strings.Contains(err.Error(), "not declared") {
		t.Fatalf("err = %v, want undeclared-key failure", err)
	}
}

func TestPublishJobOutputSecretsEmptySinkIsNoOp(t *testing.T) {
	job := outputsJob("u-empty", "WIRING=x")
	called := false
	r := &Runner{Stderr: os.Stderr}
	r.Hooks = &RunnerHooks{PublishJobOutputs: func(string, map[string]string) error {
		called = true
		return nil
	}}
	// No sink file at all (plan-only lane): no error, no publish.
	if err := r.publishJobOutputSecrets(job); err != nil {
		t.Fatalf("missing sink: %v", err)
	}
	if called {
		t.Fatal("hook called with nothing produced")
	}
}
