# Local verification — 2026-10-29

Scope: first S0/S1 offline resolver implementation, not a V0.1 release or the full
server/world recovery acceptance gate. Product scope remains the
[canonical plan](https://github.com/jkalend/project-planning/blob/main/projects/modgrid.md).
Existing local planning/README changes were retained; no planning-repository or
other ecosystem source was changed.

Environment: Windows `10.0.26200.0`, `windows/amd64`, local `E:` NTFS, Go
`go1.25.5 windows/amd64`. Java is not used by these checks. No EULA acceptance,
server boot, real world or live provider acquisition was exercised.

Inputs: project-authored `offline-v1` catalog, source revision `offline-v1`,
synthetic metadata and text payloads; no live metadata is used.

| Identity | SHA-256 |
|---|---|
| Normalized catalog | `ac3d2d3157e9a6ccc26234eb10dab42a071d1628d99b9cb414fdb23dfaa8a6f5` |
| Canonical expected lock | `9aad197a91af8eba3b653260c9efd464ea5e2b1f467fb107f5029f32dded4ba5` |

## Commands and observed results

Run from the ModGrid repository:

```powershell
go mod download
go mod verify
go vet ./...
go test ./...
go build -o bin/ ./cmd/modgrid
```

All passed locally. Tests exercise the hand-written choices/reasons, retained
canonical lock bytes, 30 reordered/repeated solves, numeric/source tie-breaking,
both sides and optional choices, successful/conflicting cycles, incompatible
tuples, missing artifacts, distinct rights/side/source failures, strict malformed
input and graph/document/search limits. A separate exhaustive assignment oracle
agrees on 60 seeded three-project/two-version dependency graphs. CLI checks cover
replacement of an existing lock, preservation after an unsatisfiable solve,
refusal to overwrite inputs and temporary cleanup after a failed rename.

```powershell
go test ./internal/resolver -run '^$' -fuzz '^FuzzResolve$' -fuzztime 20s -parallel 2
go test ./internal/resolver -run '^$' -fuzz '^FuzzStrictJSON$' -fuzztime 10s -parallel 2
```

Both bounded fuzz runs passed without a failure. The full-document run performed
only 12 executions, giving limited additional coverage. The small-input strict
JSON run performed 250,241 executions. These are smoke fuzz checks, not a
complete malformed-input or security proof.

The built Windows CLI was run through `catalog id`, two resolves (including JSON
report output), and `lock verify --expected-id` using the README fixture commands.
Both generated files and the golden lock had the expected SHA-256. The CLI
performs no live source lookup during these operations. Outputs are in ignored
`bin/` and `out/`; fixture metadata/golden files pass `git check-ignore` checks.

Linux/amd64 cross-compilation also passed with `GOOS=linux GOARCH=amd64 go build
-o out/modgrid-linux-amd64 ./cmd/modgrid` in a temporary environment. This is a
build result only. A pinned-action Windows/Linux CI matrix was added, but neither
remote CI nor Linux execution was observed in this session.

## Remaining gates

S0 is partial: real Fabric/installer metadata, exact per-platform Java 21 bytes,
authored marker-mod source/build and real boot qualification remain open. The
historical 0.19.5 tuple in the synthetic catalog is not a validated runtime pin.

S2 artifact/config-byte verification and export, S3–S7 lifecycle/checkpoint/
restore/archive/retention qualification, Linux execution, clean-host testing and
all CloudMesh/PeerVault/PersonalMesh integrations remain unimplemented or unrun.
Lock verification currently needs the frozen PackSpec/catalog and does not verify
downloaded bytes. Local lock replacement has no process-kill or power-loss
durability qualification. No runtime, recovery or integration milestone is claimed.
