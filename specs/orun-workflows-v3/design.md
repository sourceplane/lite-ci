# Design — orun-workflows-v3 (the engine comes home)

## 1. Why the boundary stops paying for itself

A process boundary between orun and the workflow engine buys exactly three
things: independent release, independent implementation language, and blast-rad­
ius isolation. Discontinuing torkflow as a product removes the first two. The
third was never real — the engine runs in orun's own process tree, reading
orun's secrets, writing orun's run directory.

What the boundary *costs* is visible in v2's own problem statement: a versioned
wire contract, credentials serialised as an unscoped blob until v2 scoped them,
structured output dying at the step boundary until v2 threaded it across, and an
engine binary that was ambient host state until v2 pinned it. **Four of v2's six
pillars are boundary-management.** They are well-built. They are also solutions
to a problem that only exists because of the boundary.

v3's thesis: delete the boundary, and those four pillars stop being code.

## 2. What actually moves

Measured against torkflow `origin/main`, non-test Go:

| Package | LOC | Disposition |
|---|---|---|
| `internal/engine` (DAG scheduler, run state) | 707 | **Vendor** → `orun/internal/workflow/engine` |
| `internal/core` (built-in handlers) | 309 | **Vendor** → `orun/internal/workflow/actions` |
| `internal/executor` (binary provider protocol) | 89 | **Drop** (see §5) |
| `internal/expression` (goja resolver) | 69 | **Vendor** |
| `internal/workflow` (file model) | 63 | **Vendor** |
| `internal/backend` + `contract/v1` | — | **Delete** (the wire) |
| `actionStore/{ai,http,demo}` | 451 | **Drop `ai`/`demo`; port `http`** (§4) |

New orun dependencies: `github.com/dop251/goja` and
`github.com/xeipuuv/gojsonschema`. `gopkg.in/yaml.v3` is already shared. goja is
a full JS interpreter — a real addition to binary size and CVE surface, accepted
because `core.js` expression evaluation is load-bearing for the file format and
the format is explicitly unchanged.

## 3. Dispatch, after

torkflow's scheduler resolves an `actionRef` in two tiers — built-in registry
first, provider binary second (`internal/engine/scheduler.go:208`). v3 keeps
tier one and removes tier two:

```go
actionRef := step.EffectiveActionRef()
handler, ok := registry.Get(actionRef)
if !ok {
    return fmt.Errorf("unknown action %s", actionRef)   // no binary fallback
}
```

**Consequence that must be closed, not inherited.** Today the built-in branch
*bypasses* input-schema validation and credential resolution — both live only on
the provider branch. With every action built-in, that bypass would silently
become the only path. v3 therefore moves both into the common path:

- Every action declares an `inputSchema`; the scheduler validates before
  invoking, built-in or not.
- Connections resolve for built-ins exactly as they did for providers. The v2
  `connections:` grant — compile-checked against the workflow's declared
  connections, mapped-only injection — is **kept verbatim**. Least privilege was
  never about the process boundary; it was about not handing an action
  credentials it did not ask for.

## 4. The built-in action set

| Action | Status in v3 |
|---|---|
| `core.exec` | **New.** §6 |
| `core.js`, `core.if`, `core.sleep`, `core.print`/`stdout`/`stdPrint` | Vendored unchanged |
| `http.request`, `http.request.auth` | **Ported** from provider binary to in-process Go, keeping the action names, input schema and `http.bearer` connection type |
| `ai.openai.chat`, `ai.anthropic.chat`, `ai.bedrock.chat`, `ai.gemini.chat` | **Dropped.** orun owns an agent/AI surface already (`specs/orun-agents*`); duplicating provider SDK clients inside the CLI is scope with a maintenance tail and no caller |
| `demo.echo` | **Dropped.** Fixture |

Dropping `ai.*` is the decision most worth challenging. It is made on the basis
that no shipped orun path references those actions, and that a workflow needing
a model call should reach orun's agent surface rather than a second, parallel AI
client stack. If a caller appears, the correct answer is a first-class orun
action backed by the existing agent plumbing — not a re-vendored provider.

## 5. Retiring the provider protocol

The `exec.Command`-per-action model (`executor.RunBinary`: JSON on stdin,
`{status, output, error, branch}` on stdout) is dropped along with
`actionStore` path discovery. This is the change that actually delivers the
stated goal — **orun ships as one binary and points at nothing.** Keeping the
engine in-process while still resolving loose executables from a filesystem path
would leave orun's distribution story *worse* than torkflow's, not better.

Third-party actions are explicitly out of scope for v3. If they become a
requirement, a plugin boundary should be designed deliberately against orun's
own policy and secrets model — not inherited from a protocol that exists because
the engine used to be foreign.

## 6. `core.exec`

The capability the whole exercise is for.

```yaml
- name: Scaffold_Foundation
  actionRef: core.exec
  parameters:
    argv: ["orun", "new", "--blueprint", "blueprints/01-foundation.yaml", "--out", "{{ Steps.Setup.dir }}"]
    cwd: "{{ Steps.Setup.repo }}"
    env: { ORUN_VERBOSE: "1" }
    timeoutSeconds: 600
  outboundEdges:
    - nextStepName: Verify_Foundation
```

Contract, deliberately narrow:

- **Argv only, never a shell.** No string command, no interpolation into a shell
  — the same rule blueprint hooks already follow (`exec.Command(h.Run[0],
  h.Run[1:]...)`, no shell).
- **Explicit `cwd`**, defaulting to the run directory, never the process cwd.
- **Env allowlist, not inheritance.** Declared keys only. A workflow that needs
  a credential asks for a connection; it does not read the ambient environment.
- **Mandatory timeout** with a bounded default; the process is killed on expiry
  and the step fails.
- **Outputs**: `exitCode`, `stdout`, `stderr`, surfaced through the v2 declared-
  outputs model so downstream steps consume typed values rather than re-parsing.
- **Non-zero exit fails the step** unless the step declares an error handler.

**Security note.** `core.exec` makes a workflow file executable code. That is
the point, and it is the same trust level as a blueprint hook or a job template
`run:` step — both of which already execute declared argv. The mitigation is
unchanged and already in place: the workflow file is **content-pinned by digest
in the plan**, so what runs is what was reviewed. Argv-only and the env
allowlist keep the blast radius declared rather than ambient.

## 7. What stays

- The **file format** (`torkflow/v1`) — byte-identical. Existing workflows and
  their digests remain valid. v3 is a runtime change, not a format change.
- **Workflow-as-plan-content**: the workflow digest in `plan.json` and
  `provenance.lock`, re-hashed and fail-closed at run time.
- **v2 §7 portability** — workflows travelling in composition OCI Stacks. This
  is orthogonal to engine location and is independently valuable now that
  compositions are OCI-distributed.
- **Resume** (v2 §8) and **declared outputs** (v2 §4), moved in-process.
- The law: **only names are intent; values are execution.**

## 8. Migration and consumers

torkflow is published to GHCR and carries a `provider.yaml`, so it has a
distribution surface and possibly consumers beyond orun. Discontinuation is a
separate, explicit step (WA6) gated on an audit of who consumes it — not a side
effect of orun vendoring the code. Until that audit clears, torkflow is frozen,
not deleted, and `ORUN_TORKFLOW_ENGINE` remains honoured as a deprecated escape
hatch that records the digest it ran.
