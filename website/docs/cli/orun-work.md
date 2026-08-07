---
title: orun work (deprecated)
---

`orun work` has been renamed to [`orun initiatives`](./orun-initiatives.md)
(specs/orun-initiatives). The old group survives one release as a hidden
alias whose subcommands (`import`, `list`, `edit`, `cancel`) forward to the
same implementations, then it is removed.

```bash
# before                         # now
orun work import specs/         orun initiatives import specs/
orun work list                  orun initiatives list
orun work edit <key>            orun initiatives edit <key>
orun work cancel <key>          orun initiatives cancel <key>
```

See [`orun initiatives`](./orun-initiatives.md) for the full group — the
portfolio, tree view, task detail, activity tail, doc pull, and design
runs. The invariant is unchanged: lifecycle is a derived query over two
append-only logs, never a stored status.
