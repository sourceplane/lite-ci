# orun-grounded-sessions — Implementation Status

As-built record for the runtime leg (GS0–GS2). The cloud leg (GS3–GS8) is
tracked in `orun-cloud/specs/epics/saas-grounded-sessions/IMPLEMENTATION-STATUS.md`.

## Summary

| ID | Milestone | Status |
|----|-----------|--------|
| GS0 | Grounded serve | ✅ Shipped |
| GS1 | `orun git-credential` | ✅ Shipped |
| GS2 | Workspace-client auth | ✅ Shipped |

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

### GS1 — `orun git-credential` (shipped)

As-built: `internal/agent/ground/token.go` (the door client + cache),
`cmd/orun/command_git_credential.go` (the helper), and the serve wiring.

- **Pull, not bake, made concrete.** Git authenticates through a helper that
  mints a repo-scoped token per operation from the session's own door. The
  helper config rides the *clone command* (`--config credential.helper=…`), so
  there is no global git state and no window in which a fetch could prompt.
- **The helper serves its binding and nothing else.** A request for another
  repository, another host, or plain http gets **silence** — git then falls
  through to its own helpers rather than failing on our account, and a hostile
  repo cannot even provoke a mint for somewhere else. The door enforces the
  same scope server-side; this is the second lock, not the only one.
- **Refusals are silent on stdout, explained on stderr.** stdout *is* the
  credential protocol, so a 403 (tier lowered, session terminal) prints
  nothing and git reports an ordinary auth failure. A 4xx is never retried; a
  5xx retries three times with backoff.
- **The cache is the only secret at rest**: `$XDG_RUNTIME_DIR/orun/`, 0600,
  atomic write, treated as spent 5 minutes before real expiry (a token that
  dies mid-clone is worse than one minted a minute early). `store` is inert —
  a credential git offers us is never persisted; `erase` clears it.
- **`GITHUB_TOKEN` seeding** for `gh`/API tools shares the helper's cache, so
  boot and the first git operation spend one mint rather than two. It goes
  stale at the token's TTL by design — git never reads it.
- The helper reads `ORUN_SESSION_TOKEN`, falling back to `ORUN_TOKEN_FILE`, so
  it keeps working across a rotation once GS2 lands.
- Tests: 21 across the door client (path, bearer, terminal-vs-retryable
  statuses, cache expiry/permissions) and the helper protocol (mint, foreign-
  request silence, cache reuse, refusal silence, inertness outside a grounded
  session, store-never-persists, erase). All against an httptest stub of the
  GS4 wire shape — no network, no credentials. `go build ./...`, `go vet`, and
  `go test ./cmd/orun/ ./internal/agent/...` green.

### GS2 — Workspace-client auth (shipped)

As-built: `internal/remotestate/auth.go` (`FileTokenSource` + the precedence
rung), `internal/agent/ground/token.go` (`TokenFilePath`, `WriteSessionToken`),
`internal/agent/attach/heartbeat.go` (`OnToken`), and the serve wiring.

- **The file is the rotation carrier, so readers re-read per call.** serve
  refreshes the session token roughly every 15 minutes; a value captured once
  at process start would be stale before a long tool call used it — the exact
  failure a static `ORUN_TOKEN` would have had. `FileTokenSource.Token()` reads
  on every call, and a test rotates the file mid-test to prove the next call
  carries the new credential.
- **One credential, not two.** The sandbox reaches the workspace as *the
  session*, so the lease that kills the session kills state, catalog, and
  secrets access at the same moment. Nothing new to revoke.
- **Precedence: `ORUN_TOKEN` > `ORUN_TOKEN_FILE` > OIDC > local session.** An
  operator's explicit override still wins over the sandbox's own plumbing.
- **Refuses a group/other-readable file** — the same discipline
  `cliauth` already enforces on the stored session — and refuses to *write* an
  empty token, since an empty file reads as "not logged in" and would send
  every in-sandbox verb down the wrong path.
- `HeartbeatConfig.OnToken` is the seam: the heartbeat already owned refresh,
  so publishing is a callback rather than a poller. serve also publishes the
  boot token (the constructor sets it directly, bypassing the setter).
- The harness env gains `ORUN_TOKEN_FILE` + `ORUN_WORKSPACE`, so any `orun`
  verb a tool call spawns authenticates as the session with zero flags.
- Tests: 9 — per-call re-read across a rotation, permission refusal, legible
  errors naming `ORUN_TOKEN_FILE`, both precedence rungs, file round-trip with
  0600 + atomic overwrite, empty-token refusal, and path resolution.
  `go build ./...`, `go vet`, and `go test ./cmd/orun/ ./internal/agent/...
  ./internal/remotestate/` green.
