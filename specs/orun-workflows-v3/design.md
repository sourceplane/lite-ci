# Design — orun-workflows-v3 (one language, one binary)

## 1. The review verdict: absorb the semantics, not the artifact

Rev 1 planned to vendor torkflow's engine and file format as-is. Reviewing the
actual code (`torkflow origin/main`) against orun's own models showed why that
is the wrong bar:

- **The step model duplicates orun's, worse.** torkflow steps carry
  `actionRef` *and* `actionID` (legacy dual), `metadata.name` *and* `id`,
  push-model `outboundEdges` with branch names, and a `core.if` action for
  branching. orun's DAGs — plans, blueprints, phases — are all pull-model
  (`dependsOn`/`needs`), and orun steps already have a settled verb set
  (`run`/`use`/`workflow` + `with`/`env`/`timeout`/`retry`). Absorbing the
  torkflow vocabulary means two step languages in one binary forever.
- **Workflows are not parameterizable.** `WorkflowSpec` has `connections`,
  `outputs`, `maxParallelSteps` — no `inputs`. Blueprints solved typed inputs
  (`InputSpec`: type/required/default/pattern/values/secret). A flow you cannot
  parameterize cannot be a reusable golden path.
- **The expression contract is "whatever goja accepts."** Interpolation and
  skip-conditions evaluate through a full JS interpreter. There is no grammar
  to validate at compile time, no termination guarantee, and the dependency is
  a real binary-size and CVE cost.
- **The installed base rounds to zero.** A handful of example files; the
  orun↔torkflow integration first executed end-to-end weeks ago. Byte-identical
  compatibility (rev 1's promise) preserved a museum at the cost of carrying
  every defect above.

So v3 keeps torkflow's **behavioural** achievements — DAG scheduling, declared
outputs (WX4), file-backed resume (WX6), the scoped connections grant (WX2) —
and re-homes them behind orun's own language. `orun workflow convert`
mechanically translates old files; `torkflow/v1` is read-supported through a
deprecation window and never authored again.

## 2. The language

```yaml
apiVersion: orun.dev/v1          # matches kind: Blueprint, the sibling artifact
kind: Workflow
metadata:
  name: <slug>                    # one name; no duplicate id field
inputs:                           # REUSED: scaffold.InputSpec, verbatim
  <name>: { type, required, default, pattern, values, secret, description }
connections:                      # declared connection names (v2 grant model)
  <name>: { type: http.bearer }
outputs:                          # name → CEL expression over final state
  <name>: "<expr>"
steps:
  - name: <slug>
    needs: [<step>, ...]          # pull edges — the only edge form
    if: "<CEL>"                   # optional guard; false ⇒ skipped
    # exactly one execution verb:
    run: ["argv0", "arg1", ...]   #   in-process exec — argv only, never a shell
    action: <pkg>.<name>          #   built-in or resolved action package
    workflow: <path-or-ref>       #   nested workflow (cycle-checked at compile)
    with: { ... }                 # parameters (interpolated, schema-validated)
    env: { KEY: "value" }         # run: only — allowlist, never inheritance
    cwd: "<path>"                 # run: only — defaults to the run dir
    connection: <name>            # from the declared connections
    timeout: 10m                  # mandatory ceiling (default applied if absent)
    retry: { maxRetries: 3, baseDelay: 5s, kind: exponential }
    continueOnError: false
    poll: { interval: 30s, timeout: 20m, until: "<CEL>" }   # §7
    outputs:
      <name>: "<CEL over this step's result>"
```

Deliberate deletions from torkflow/v1 and their replacements:

| torkflow/v1 | v3 |
|---|---|
| `outboundEdges` + `branchName` | `needs:` + `if:` on the downstream step. Branching is structural, not an action |
| `core.if` | `if:` |
| `actionRef`/`actionID` | `action:` |
| `skip` expression | `if:` (inverted, one polarity) |
| `readinessGate.thresholdType` | dropped — `needs` is AND-join; anything fancier waits for a real use case |
| `fallbackStepName` | `continueOnError` + an `if: "steps.x.failed"` downstream step |
| `metadata.id` | gone |

References are `{{ inputs.<n> }}`, `{{ steps.<name>.outputs.<n> }}`, and
`{{ connections.<n> }}` — the same names the v2 grant already compile-checks.
Every reference is validated at compile time against declared names; the values
remain sealed run facts. **Only names are intent; values are execution.**

## 3. Expressions: CEL is the contract

`if:`, `until:`, `outputs:` expressions and `{{ }}` interpolation are
**CEL** (`cel-go`): side-effect-free, non-Turing-complete, terminating,
compile-checkable — the properties a digest-pinned control-flow language must
have, proven at scale by Kubernetes admission policy. Compile errors surface in
`orun workflow validate`, not at run time.

goja does not disappear — it retreats to where arbitrary computation is
legitimately wanted: a **`script` built-in action** whose body is JS over its
`with:` inputs, returning a value that becomes the step result. The interpreter
loads lazily; a workflow that never uses `script` never pays for it. What v3
refuses is JS as the *ambient* language of conditions and interpolation.

## 4. Dispatch and the action tiers

One dispatch path, one contract (this closes a live gap: today the built-in
branch bypasses input-schema validation and credential resolution — both exist
only on the provider branch, `scheduler.go:208`):

```
verb run:      → in-process exec (§6). No registry involved.
verb action:   → registry.Resolve(ref) → schema-validate(with) →
                 inject mapped connections only → invoke
verb workflow: → compile + run nested (its digest folded into the parent's pin)
```

**Tier 1 — built-ins** (compiled into orun): `http.request` (+ `http.bearer`
connection type, ported from the provider binary — it was never in torkflow's
binary either, contrary to appearances), `script` (goja, lazy), `sleep`.
Every built-in declares an `inputSchema` and is validated like anything else.
`ai.*` is not ported: orun owns an agent surface; a model call from a workflow
should be a first-class orun action backed by that plumbing, added when a
caller exists. `demo.echo` dies.

**Tier 2 — action packages** (§5): namespaced `<pkg>.<action>`, resolved from
OCI, digest-locked. The registry namespace is designed for this now even though
tier 2 ships later — built-ins are just the pre-resolved packages `http`,
`script`, `core`.

## 5. The store, redesigned: a registry, not a directory

torkflow's `actionStore` is a filesystem path of loose executables discovered
at run time (`firstExistingDir(req.ActionStores)`, JSON over stdin/stdout).
Rev 1's answer — delete it, everything built-in — was reviewed and rejected as
one-sided: it makes every future action a fork of orun.

The v3 store concept:

- **An action package is an OCI artifact**, `oci://ghcr.io/<org>/orun-actions/
  <pkg>:<version>`: a manifest (action names, input schemas, connection types)
  plus an implementation payload. Resolved, cached content-addressed under
  `~/.orun/cache`, and **locked by digest through the same
  `internal/composition` machinery that pins composition sources** — one
  resolution story for everything that travels.
- **The payload is WASM-first** (executed via `wazero` — pure Go, no cgo, no
  process spawn): sandboxed by construction, cross-platform by construction,
  capability-scoped (an action gets exactly the host functions the manifest
  grants: http egress, nothing else). The stdin/stdout JSON *process* protocol
  is retained only as a documented fallback payload type for actions that
  genuinely need a native binary, and such packages are marked `native: true`
  in the manifest so policy can refuse them.
- **The plan pins what ran.** A workflow step using `slack.post` materializes
  the package's digest into `plan.json`/`provenance.lock` beside the workflow
  digest — "which action ran" is never ambient, which was v2 §6's goal for the
  engine, now applied one level down where it still has meaning.

What this deletes: `actionStore` path discovery, `ActionModule` yaml-on-disk,
the loose-binary distribution story, and rev 1's closed-world assumption — in
both directions.

## 6. `run:` — the exec verb, not an exec action

Semantics, matching blueprint hooks (which already execute declared argv with
no shell):

- **argv array only.** No shell, ever. A `;` or `$( )` in an argument is a
  literal. Anyone needing a shell writes `["bash", "-lc", "..."]` explicitly
  and owns that choice visibly in review.
- `cwd` defaults to the step's run dir, never the process cwd.
- `env` is an **allowlist over a fixed hygienic base** (`PATH`, `HOME`,
  `TMPDIR` — without which no real tool runs) plus the declared keys; nothing
  else is inherited. Credentials arrive via `connection:`, not the ambient
  environment.
- `timeout` mandatory (bounded default), kill on expiry.
- Result fields available to `outputs:`/`until:` expressions: `exitCode`,
  `stdout`, `stderr`. Non-zero exit fails the step unless `continueOnError`.

Job-template `run:` (a shell string executed by a CI runner) is untouched —
shell where a runner shell exists, argv where orun executes in-process. The
asymmetry is deliberate and documented.

## 7. `poll:` — the missing primitive

The motivating flow — scaffold → PR → **wait for CI** → merge — is a polling
loop. Rev 1 had no way to express it short of a bash sleep loop inside exec,
which defeats visibility, timeout policy and resume. v3 makes it structural:

```yaml
poll: { interval: 30s, timeout: 20m, until: "output.status == 'completed'" }
```

The step's verb re-executes on `interval` until `until` evaluates true
(success), `timeout` expires (failure), or the verb itself errors terminally.
Each attempt is a recorded run fact; resume treats an in-flight poll as
not-succeeded and re-enters it. Human approval remains where v2 §9 put it — on
the **calling** orun step, sealed as a run fact — a workflow pauses by ending a
`poll:` on a condition a human satisfies (a PR merged, a check green), not by
re-implementing approvals inside the engine.

## 8. Engine internals: vendor the tests, own the code

What moves in is the **contract**: DAG execution with `needs` joins,
per-step retry/backoff, `continueOnError`, declared outputs, file-backed state
under `.orun/wfruns/<execId>/` with resume-from-non-succeeded (WX6 semantics).
The scheduler implementation (~700 LOC, channel+mutex with a 50 ms polling
fallback and one fixed data race in its history) may be vendored or rewritten
at the implementer's discretion — **torkflow's scheduler and race tests come
across regardless and must pass**. Semantics are sacred; internals are not.

## 9. Connections and secrets

torkflow's `connections.yaml`/`secrets.yaml` file pair dies with the engine —
plaintext credentials on disk (a live token was sitting in a working copy
during this review) have no successor. A workflow declares connection *names*;
the calling surface (step or hook `connections:` grant) maps each name to
`secret://` references resolved through orun's secrets machinery at run time,
mapped-only, exactly as v2 shipped. In-memory only, redacted in logs, never in
run-state files. A **standalone** `orun workflow run` (no calling step to carry
a grant) supplies the same mapping via `--connection <name>.<field>=<secret://ref|value>`
— the flag is the grant.

## 10. Trust model

`run:` makes a workflow file executable code, and v2 §7 makes workflow files
travel in OCI stacks. This is the **same** trust surface compositions already
have — an OCI-distributed job template carries arbitrary `run:` shell today —
and it gets the same answer: content digests pinned in the plan, so what was
reviewed is what runs; argv-only and env-allowlisting keep the blast radius
declared; tier-2 actions add the WASM sandbox and per-package capability
grants on top. No new trust category is created.

## 11. What v3 deletes (unchanged from rev 1)

`contract/v1` wire schemas and fixtures · torkflow `backend` mode · engine
digest pinning and OCI engine resolution (v2 §6) · `ORUN_TORKFLOW_ENGINE`
(deprecated during migration, then removed) · the `exec.Command` provider
protocol and `actionStore` discovery · `orun workflow engine-digest`. The
engine's digest is the orun binary's digest.

## 12. Migration — there is none (WA0 decision)

Product decision, recorded here: **no backward compatibility.** The installed
base rounds to zero, so:

- `torkflow/v1` files are **rejected** with an error naming this spec. No
  converter ships; the §2 mapping table is the manual migration guide for the
  handful of example files that exist.
- `execution.workflowEngine` in an intent is an **error** immediately.
- `ORUN_TORKFLOW_ENGINE` is deleted, not deprecated.
