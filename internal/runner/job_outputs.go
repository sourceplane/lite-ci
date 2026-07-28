package runner

// Job output secrets (SEC-JOB, de-AWS v2): the producer half of the
// lease-bound wiring channel.
//
// A job DECLARES the keys it may publish via its `secretOutputs` parameter
// ("KEY=producer-hint,KEY2=…" — the runner reads only the KEY halves; the
// hint half belongs to the composition, e.g. a terraform output name). Steps
// PRODUCE values by appending to the $ORUN_SECRET_OUTPUTS sink file the
// runner exports — no CLI, no auth, no scope plumbing in shell:
//
//   KEY=single-line-value
//   KEY<<__ORUN_EOF__
//   multi
//   line value
//   __ORUN_EOF__
//
// After the job's steps succeed the runner parses the sink, enforces the
// declared-keys allow-list (an undeclared key fails the job — no silent
// side-channel writes), registers every value with the redactor, and hands
// the batch to the PublishJobOutputs hook — which, on remote runs, posts it
// over the run's LEASE-bound channel; the platform derives the target scope
// (project/env rung) from the leased job itself. A job that produced nothing
// publishes nothing (plan-only lanes share the component's parameters, so an
// empty sink is normal, not an error).

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sourceplane/orun/internal/model"
)

const secretOutputsParameter = "secretOutputs"

var secretOutputKeyRE = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)

// jobSecretOutputKeys returns the declared publishable keys of a job — the
// KEY halves of its `secretOutputs` parameter. Empty when undeclared.
func jobSecretOutputKeys(job model.PlanJob) []string {
	raw, ok := job.Parameters[secretOutputsParameter]
	if !ok {
		return nil
	}
	spec, ok := raw.(string)
	if !ok {
		return nil
	}
	keys := make([]string, 0, 4)
	for _, pair := range strings.Split(spec, ",") {
		key := strings.TrimSpace(pair)
		if eq := strings.IndexByte(key, '='); eq >= 0 {
			key = key[:eq]
		}
		if key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

// jobSecretOutputsSinkPath is deterministic per job so every step of the job
// appends to the same file.
func (r *Runner) jobSecretOutputsSinkPath(job model.PlanJob) string {
	uid := strings.Map(func(c rune) rune {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' {
			return c
		}
		return '-'
	}, job.UID)
	return filepath.Join(os.TempDir(), "orun-secret-outputs-"+uid)
}

// parseSecretOutputsSink parses the sink format: KEY=value lines and
// KEY<<DELIM heredocs. Unrecognized non-empty lines are an error — a garbled
// sink must never be silently dropped (the value it garbled may be needed).
func parseSecretOutputsSink(content string) (map[string]string, error) {
	out := map[string]string{}
	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		if hd := strings.Index(line, "<<"); hd > 0 {
			key := strings.TrimSpace(line[:hd])
			delim := strings.TrimSpace(line[hd+2:])
			if delim == "" {
				return nil, fmt.Errorf("secret output %q: empty heredoc delimiter", key)
			}
			var value []string
			closed := false
			for i++; i < len(lines); i++ {
				if strings.TrimSpace(lines[i]) == delim {
					closed = true
					break
				}
				value = append(value, lines[i])
			}
			if !closed {
				return nil, fmt.Errorf("secret output %q: unterminated heredoc", key)
			}
			out[key] = strings.Join(value, "\n")
			continue
		}
		if eq := strings.IndexByte(line, '='); eq > 0 {
			out[strings.TrimSpace(line[:eq])] = line[eq+1:]
			continue
		}
		// Never echo the offending line — it may BE a secret value.
		return nil, fmt.Errorf("secret outputs sink: line %d is neither KEY=value nor KEY<<DELIM", i+1)
	}
	return out, nil
}

// publishJobOutputSecrets runs after a job's steps succeed. Missing/empty
// sink → nothing produced → no-op.
func (r *Runner) publishJobOutputSecrets(job model.PlanJob) error {
	declared := jobSecretOutputKeys(job)
	if len(declared) == 0 {
		return nil
	}
	sink := r.jobSecretOutputsSinkPath(job)
	defer func() { _ = os.Remove(sink) }()
	content, err := os.ReadFile(sink)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read secret outputs sink: %w", err)
	}
	if strings.TrimSpace(string(content)) == "" {
		return nil
	}
	entries, err := parseSecretOutputsSink(string(content))
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}

	// The declared list is an ALLOW-list: a step publishing a key the job
	// never declared is a fail-loud contract violation, not a warning.
	allowed := make(map[string]struct{}, len(declared))
	for _, k := range declared {
		allowed[k] = struct{}{}
	}
	for key, value := range entries {
		if !secretOutputKeyRE.MatchString(key) {
			return fmt.Errorf("secret output %q: keys must be UPPER_SNAKE", key)
		}
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("secret output %q is not declared in %s", key, secretOutputsParameter)
		}
		if value == "" {
			return fmt.Errorf("secret output %q: empty value", key)
		}
	}

	// Redact BEFORE anything can echo the values.
	if r.Redactor != nil {
		values := make([]string, 0, len(entries))
		for _, v := range entries {
			values = append(values, v)
		}
		r.Redactor.Add(values...)
	}

	if r.Hooks == nil || r.Hooks.PublishJobOutputs == nil {
		fmt.Fprintf(r.Stderr, "  · %s: %d secret output(s) produced but no backend to publish (local run) — skipped\n", job.ID, len(entries))
		return nil
	}
	if err := r.Hooks.PublishJobOutputs(job.ID, entries); err != nil {
		return fmt.Errorf("publish %d secret output(s): %w", len(entries), err)
	}
	fmt.Fprintf(r.Stderr, "  · %s: published %d secret output(s)\n", job.ID, len(entries))
	return nil
}
