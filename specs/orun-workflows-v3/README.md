# Spec: orun-workflows-v3 (one language, one binary)

**v1 made a workflow runnable. v2 made workflows how the platform talks. v3
makes them speak orun.** torkflow is being discontinued as a standalone
product; its engine comes home. But v3 is not a transplant — rev 2 of this epic
concluded that absorbing torkflow *as-is* would import three things orun should
not want: a second step vocabulary (`actionRef`/`outboundEdges`) alien to the
one orun already has, a JS interpreter as the de-facto expression *contract*,
and a filesystem store of loose executables as the extension model.

v3 instead vendors the engine's **semantics** — DAG execution, declared
outputs, file-backed resume, the connections grant — behind orun's **own
language**: a `kind: Workflow` whose steps use the same verbs as job templates
and blueprint hooks (`run:` argv, `action:` + `with:`, nested `workflow:`),
whose edges are pull-model `needs:` like every other DAG in the product, and
whose inputs reuse the Blueprint `InputSpec` verbatim.

## Rev 2 — what the review changed and why

| Rev 1 said | Rev 2 says | Because |
|---|---|---|
| Add `core.exec` action | **`run:` is a step verb, not an action** | orun already spells "execute argv" three ways (job steps, hooks, now actions). A fourth spelling is vocabulary debt. One verb, everywhere |
| Keep `torkflow/v1` byte-identical | **New `orun.dev/v1 kind: Workflow`; `orun workflow convert` for old files** | The installed base is a handful of example files. Compatibility preserved a museum; convertibility is the honest constraint. Push-edges, `actionRef`/`actionID` duplication, and un-parameterizable workflows should not be carried for zero consumers |
| goja stays as the expression engine | **CEL for control flow; goja only inside a `script` action** | Conditions and interpolation must be side-effect-free and terminating — the property CEL was built for and Kubernetes proved. "Whatever a JS engine accepts" is not a contract |
| Drop the actionStore, everything built-in | **Built-ins now; OCI action packages (WASM-first) as the designed extension seam** | "All built-in forever" closes the door. The modern store is not a directory — it is the registry: content-addressed, digest-locked by the same `internal/composition` machinery that already pins compositions, executed in a sandbox |
| *(absent)* | **`poll:`/`until:` wait primitive** | Rev 1 could not express its own acceptance test. "Open PR → wait for CI green → merge" requires first-class polling, not bash sleep loops smuggled through exec |
| *(implicit)* | **torkflow's `connections.yaml`/`secrets.yaml` files die** | Plaintext secrets on disk (a live token was found in a working copy during this review). Connections resolve exclusively through orun's secret machinery (`secret://`), per the v2 grant |

Unchanged from rev 1: the boundary dies (`contract/v1`, `backend` mode, engine
digest pinning, `ORUN_TORKFLOW_ENGINE`); the trade — discarding recently
shipped boundary code — is accepted and stated, not minimised; v2 §7
(workflows travel in OCI Stacks) survives; and the law is untouched: **only
names are intent; values are execution.**

## What a workflow looks like after v3

```yaml
apiVersion: orun.dev/v1
kind: Workflow
metadata:
  name: baseline-flow
inputs:
  repoName: { type: string, required: true, pattern: "^[a-z][a-z0-9-]*$" }
steps:
  - name: scaffold
    run: ["orun", "new", "--blueprint", "blueprints/01-foundation.yaml",
          "--out", "{{ inputs.repoName }}", "--set", "repoName={{ inputs.repoName }}"]
  - name: open-pr
    needs: [scaffold]
    run: ["gh", "pr", "create", "--fill"]
    outputs:
      url: "{{ stdout.trim() }}"
  - name: wait-ci
    needs: [open-pr]
    action: http.request
    with: { url: "{{ steps.open-pr.outputs.url }}/checks", method: GET }
    poll: { interval: 30s, timeout: 20m, until: "output.status == 'completed'" }
  - name: merge
    needs: [wait-ci]
    if: "steps.wait-ci.outputs.conclusion == 'success'"
    run: ["gh", "pr", "merge", "--squash"]
```

No engine binary. No action store on disk. No second vocabulary.

## Status

Proposed (rev 2 supersedes rev 1 in place; rev 1 was merged as #563 and its
delta is recorded above). Supersedes `orun-workflows-v2` §6 and reverses its
*"in-process engine import"* non-goal — recorded in WA0, with product sign-off
on torkflow's discontinuation as the gate.

## Read order

1. `design.md` — the language, the dispatch, the store-as-registry, trust.
2. `implementation-plan.md` — WA0–WA7 in dependency order.

## Out-of-band references

- `specs/orun-workflows/`, `specs/orun-workflows-v2/` — v1/v2; v2 §7 is kept.
- torkflow `origin/main` `internal/{engine,core,expression,state,dag}` — the
  semantics and tests that move; the scheduler internals may be rewritten.
