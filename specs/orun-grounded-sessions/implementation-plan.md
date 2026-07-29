# orun-grounded-sessions — Implementation Plan

Status: Draft. Cross-repo sequencing: cloud GS3 (contracts/env) and GS4 (the
door) land first; GS0/GS1 develop against a fake relay serving the GS4 wire
shape (the AL fixture pattern); GS2 can land any time after GS0.

## GS0 — Grounded serve

- `cmd/orun/command_agent_serve.go`: the grounding step (design §1) between
  relay-up and brief assembly; env parsing (`ORUN_REPO_REMOTE` /
  `ORUN_REPO_FULL_NAME` / `ORUN_REPO_REF`); workdir handoff via the existing
  `RunOptions.Workdir`.
- `internal/agent/ground/` (new small package): clone/fetch/branch logic,
  resume-safe dir validation, stage-tagged errors — unit-testable without a
  sandbox (local bare-repo fixtures, no network).
- Failure taxonomy per design §1 step 7.

**Done when:** with `ORUN_REPO_*` set and a local bare repo as remote, serve
clones bloblessly at the ref, creates `agent/<task>-<type>`, and the driver
brief's workdir is inside the checkout (asserted via the stub driver); with
the env absent, serve's behavior is byte-identical to today (golden test on
the boot log); each failure stage produces its tagged error and a non-zero
exit; a second boot over the same dir (resume) fetches instead of cloning and
refuses a mismatched remote.

## GS1 — `orun git-credential`

- `cmd/orun/command_git_credential.go`: hidden subcommand, protocol per
  design §2; door client in `internal/agent/ground` (shared HTTP + retry
  posture with the relay).
- Cache + single-flight lock; `erase` clears it.
- Serve wires the helper via clone-time `--config`, and seeds `GITHUB_TOKEN`
  into the harness env at driver launch.

**Done when:** against a fake door (httptest serving the GS4 wire shape),
`echo -e "protocol=https\nhost=github.com\npath=owner/repo.git" | orun
git-credential get` prints the `x-access-token` pair; a mismatched path
prints nothing; the second call inside the TTL window hits the cache (door
called once — asserted); 403 prints nothing and exits 0 (git-clean failure);
502 retries 3×; parallel invocations mint once (lock test); an end-to-end
`git fetch` against a local HTTP git server authenticates through the helper.

## GS2 — Workspace-client auth

- `internal/remotestate/auth.go`: the `ORUN_TOKEN_FILE` source in
  `ResolveAuth`, precedence `ORUN_TOKEN` > file > OIDC > session; call-time
  read; permission check (refuse group/other-readable).
- Serve: atomic write on every rotation; export `ORUN_TOKEN_FILE` +
  `ORUN_WORKSPACE` to the harness env.

**Done when:** `ResolveAuth` unit tests cover precedence, call-time re-read
(rotate the file mid-test, next call carries the new token), and the
permission refusal; in the serve integration test the file tracks a token
rotation; `orun cloud check` against a fake backend authenticates via the
file with no flags and lands on `ORUN_WORKSPACE`'s scope.
