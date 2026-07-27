# orun-workflows-v3 — implementation status

| Milestone | Status | What shipped |
|---|---|---|
| **WA0** | ✅ #565 | Decision record: v2 §6 superseded, non-goal reversed, **no backward compatibility** (torkflow/v1 rejected outright, engine pin errors immediately); run: env = allowlist over PATH/HOME/TMPDIR; standalone `--connection` grant |
| **WA1** | ✅ #566 | `internal/flow`: the language — kind: Workflow (orun.dev/v1), typed inputs, pull-model needs, CEL everywhere, real validate (DAG/verbs/actions/expressions/references incl. transitive-upstream visibility), digest, built-in registry (http.request/script/sleep) |
| **WA2** | ✅ #567 | The in-process engine: event-driven scheduler (no polling), skip/block/continueOnError, retry+backoff, file-backed resume re-executing only non-succeeded steps with digest-change refusal, declared-outputs-only sealing, nested workflows with cycle detection; run/view in-process |
| **WA3** | ✅ #568 | The run: verb — argv-only (metachars provably literal), hygienic env allowlist (planted-secret leak test), run-dir default cwd, prompt timeout kill, 4 MiB output caps, exit/stdout/stderr as CEL results |
| **WA4** | ✅ #569 | poll: — interval/timeout/until over each attempt's result; run: non-zero exits are evaluable attempts (typed ExitErr — the gh-pr-checks shape); poll+retry mutually exclusive |
| **WA5** | ✅ #570 | The boundary deleted: internal/workflowbackend gone (contract/v1, backend wire, ORUN_TORKFLOW_ENGINE, provider protocol); runner/planner/scaffold rewired to internal/flow; plan-time workflow validation; engine pin removed end to end (intent declaring it fails with the §12 error); grants+output-refs re-homed with byte-identical digests |
| **WA6** | ⏸ deferred by design | OCI action packages (WASM-first store). Nothing depends on it; the registry namespace is already shaped for it. Ship when the first real third-party action appears |
| **WA7** | ◐ proof passed; archival pending | `lumen/blueprints/baseline-flow.yaml` — 8 nodes (7 scaffold phases + verify) — ran end to end in-process: **planJobs = 97**, identical to the baseline, resolved from the OCI stack. Remaining: the PR/poll-CI/merge live variant, the induced-kill resume rerun, and torkflow archival (gated on consumer audit + product sign-off) |
