# orun-grounded-sessions — Implementation Status

As-built record for the runtime leg (GS0–GS2). The cloud leg (GS3–GS8) is
tracked in `orun-cloud/specs/epics/saas-grounded-sessions/IMPLEMENTATION-STATUS.md`.

## Summary

| ID | Milestone | Status |
|----|-----------|--------|
| GS0 | Grounded serve | ✅ Shipped |
| GS1 | `orun git-credential` | 🗓️ Planned |
| GS2 | Workspace-client auth | 🗓️ Planned |

## Notes

- Spec authored 2026-07-29 alongside the cloud epic.

### GS0 — Grounded serve (shipped)

As-built: `internal/agent/ground` (`Detect` + `Ground` + `StageError`) and the
serve wiring in `cmd/orun/command_agent_serve.go`.

- Grounding runs after the heartbeat and relay are up and before the object
  store / brief / driver setup, exactly as design §1 specifies: a failure is
  reportable (the console sees the session fail with its stage) and the driver
  receives the final workdir.
- The task-keyed branch computation **moved earlier** in serve (it is a pure
  function of type+task) so grounding and `RunOptions` share one string.
- Resume safety is by remote match: a non-empty workdir must be a git repo
  whose `origin` is the bound remote, else `prepare` refuses — grounding never
  adopts a directory it does not own. An **empty** directory is not foreign and
  is cloned into.
- Branch reuse never clobbers: a resumed session's task branch is checked out
  as-is, so commits the agent already made survive (regression-tested).
- **Found and fixed en route:** `WriteMCPConfig` returns a *relative*
  `.orun/agent-mcp/mcp.json`, and the harness runs with `cmd.Dir =
  Brief.Workdir`. Setting a grounded workdir would therefore have resolved
  `--mcp-config` inside the checkout and silently dropped the entire tool
  plane. serve now absolutizes the path before handing it to the driver. This
  was latent — no ungrounded session ever set a workdir, so it had never
  fired.
- Deliberately **not** done here: no chdir into the checkout (the object store
  and `agents/*.md` resolution stay on serve's cwd — changing that is a
  behavioral decision no milestone has taken), and no credential helper
  (`Options.CredentialHelper` exists and rides the clone command, unset until
  GS1).
- Tests: 16 in `internal/agent/ground` over local bare-repo fixtures — clone
  at ref, blobless promisor, branch create/reuse, resume-fetch, foreign and
  non-git workdir refusal, every failure stage, the `repo_clone_failed:`
  terminal marker, and credential redaction. No network, no credentials.
  `go build ./...`, `go vet`, and `go test ./cmd/orun/ ./internal/agent/...`
  green.
