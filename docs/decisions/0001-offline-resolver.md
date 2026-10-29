# D-0001: Go CLI and bounded offline resolver

Accepted for the first implementation, 2026-10-29.

The [canonical design](https://github.com/jkalend/project-planning/blob/main/projects/modgrid.md)
and [S0/S1 delivery plan](https://github.com/jkalend/project-planning/blob/main/projects/modgrid-release-plan.md)
call for an independently usable offline resolver before server lifecycle work.
Use one private Go module, `modgrid.local/modgrid`, with the tested Go 1.25.5
toolchain. The CLI uses the standard library for flags, strict JSON shape checks,
hashes and temporary-file publication. There are no sibling source imports.

Use the Apache-2.0 [cyberphone JCS implementation](https://github.com/cyberphone/json-canonicalization)
at commit `19d51d7fe467d4706a3ff08adf8a748f29fc21e0`, pinned with module checksums.
Its [license](https://github.com/cyberphone/json-canonicalization/blob/19d51d7fe467d4706a3ff08adf8a748f29fc21e0/LICENSE)
permits this dependency; retain its notices/license when distributing binaries.
RFC numeric and UTF-16 property-order vectors test the selected implementation.
The application rejects invalid UTF-8 and unpaired surrogates before Go decoding
and restricts numeric input to exact nonnegative integers. Plain JSON marshaling
alone does not define lock identity.

Evaluated the documented interface of [contriboss/pubgrub-go](https://github.com/contriboss/pubgrub-go):
it offers semantic ranges, conflict derivation and a step watchdog. Those features
do not remove ModGrid's need to define source, file, optional and side identity.
For this catalog of at most 64 projects/256 artifacts, a deterministic bounded
backtracking implementation is the smaller first engine and directly controls
candidate order and source-fact diagnostics. This is an interface evaluation,
not an executed comparison or a finding that PubGrub cannot fit. Reconsider a
conflict-learning solver when real supported catalogs hit the decision ceiling.

S1 stores normalized fixture metadata and an explicit catalog digest, without a
CAS, network adapter, archive parser or runtime process. A lock is a versioned
local contract, not a published cross-project API. Later runtime/checkpoint
formats must be specified when their implementations arrive. The historical
loader candidate is not promoted to a qualified runtime pin by this decision.
