# Vendored wire contract

This directory holds **verbatim** copies of normative contracts owned by the
platform repo:

    orun-cloud/specs/epics/saas-orun-platform/state-api-contract.md
    orun-cloud/packages/mcp → mcp-tool-manifest.json  (MCP9 export)

The platform repo owns the normative copies; this repo vendors them so that
the `internal/remotestate` client (state contract) and the
`internal/platformmcp` tool plane (tool manifest — orun-mcp design §4) can be
developed and reviewed against stable, checked-in versions of the seams.
Neither repo may break a contract unilaterally (see
`specs/orun-cloud/README.md`).

`mcp-tool-manifest.json` is additionally copied byte-for-byte into
`internal/platformmcp/` for `go:embed`; `TestEmbeddedManifestMatchesVendored`
and `TestVendoredManifestChecksum` (internal/platformmcp/parity_test.go) pin
the copy and the CHECKSUM entry. Re-vendor procedure: copy the new export
here AND into `internal/platformmcp/`, update `CHECKSUM`, reconcile
`cededToWorkPlane` (below), run the parity tests.

### Tools this repo cedes to the work plane

The manifest is the TS plane's whole roster, and the TS plane serves some
tools this repo already serves natively. `internal/platformmcp` therefore
advertises a **subset**: `cededToWorkPlane` (internal/platformmcp/manifest.go)
lists the names `internal/workmcp` owns, and the platform provider drops
them from `tools/list` and disowns them at dispatch.

As of the 29-tool manifest that is four work-plane reads — `initiatives_list`,
`initiative_tree`, `task_get`, `activity_get` — leaving 25 advertised
(19 reads + 6 writes), which is why the platform-plane counts in
`specs/orun-mcp` and the website docs did not move when the manifest grew.

This is not optional bookkeeping: `mcpserve.checkRoster` rejects a roster
carrying one name twice, so an unceded duplicate makes `orun mcp serve` fail
at startup as soon as both planes mount. **Every re-vendor must ask whether
the new export added a name `internal/workmcp` already serves.**
`TestCededNamesResolveInTheManifest` catches the opposite drift — a ceded
name the manifest no longer carries.

## Drift guard

`CHECKSUM` records the sha256 of `state-api-contract.md`. The Go test
`TestVendoredContractChecksum` in
`internal/remotestate/contract_vendor_test.go` recomputes the digest of the
vendored file and fails the build if it no longer matches `CHECKSUM`.

This is an **in-repo integrity guard**: it catches an accidental or
unreviewed edit to the vendored copy. A true cross-repo live diff against the
platform repo's source needs orun-cloud access at CI time, which is not
guaranteed in this repo's CI; the guard here is the portable equivalent. If a
cross-repo fetch/vendor mechanism is later added to this repo, fold this guard
into it.

## Re-vendor procedure

When the platform repo changes the contract (additively or with a contract
version bump per the platform's change-control), re-vendor here:

1. Copy the new source verbatim:

       cp ../../../../orun-cloud/specs/epics/saas-orun-platform/state-api-contract.md \
          specs/orun-cloud/vendored/state-api-contract.md

   (adjust the source path to wherever the platform repo is checked out).

2. Recompute and record the new checksum in `CHECKSUM`:

       sha256sum specs/orun-cloud/vendored/state-api-contract.md
       # paste "<sha256>  state-api-contract.md" into CHECKSUM (replacing the old line)

3. Update `internal/remotestate` (and tests) for any contract change, run
   `go test ./...`, and commit the re-vendor together with the client change
   so the diff documents the contract delta that motivated it.

If `TestVendoredContractChecksum` fails unexpectedly, it means the vendored
file changed without the checksum being updated — either revert the edit, or
**re-vendor from orun-cloud or renegotiate the contract**, then update
`CHECKSUM`.

### Re-vendoring `mcp-tool-manifest.json`

Same shape, one extra step:

1. Copy the new export verbatim, here and into the embed directory:

       cp ../orun-cloud/packages/mcp/tool-manifest.json \
          specs/orun-cloud/vendored/mcp-tool-manifest.json
       cp specs/orun-cloud/vendored/mcp-tool-manifest.json internal/platformmcp/

2. Record the new digest in `CHECKSUM`:

       sha256sum specs/orun-cloud/vendored/mcp-tool-manifest.json
       # paste "<sha256>  mcp-tool-manifest.json" into CHECKSUM

3. **Reconcile `cededToWorkPlane`.** Diff the new tool names against
   `workmcp.Tools()`; any name both planes serve must be ceded, or
   `orun mcp serve` fails at startup on the duplicate.

4. Update the roster counts asserted in `internal/platformmcp/parity_test.go`
   (manifest total and reads, advertised total and reads) and run
   `go test ./internal/platformmcp/... ./cmd/orun/...`.
