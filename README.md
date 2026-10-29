# ModGrid

> Reproducible modpack resolution and dedicated Minecraft server orchestration.

**Status: implementation toward standalone 1.0.** The offline resolver, verified
artifact cache/export and paired checkpoint/restore engine are implemented.
Real-runtime qualification and later product stages remain in progress.

Canonical design: [modgrid](https://github.com/jkalend/project-planning/blob/main/projects/modgrid.md)

Local delivery map: [implementation and release roadmap](docs/implementation-plan.md).
Detailed stages through standalone 1.0, integration releases and maintenance:
[project-planning release plan](https://github.com/jkalend/project-planning/blob/main/projects/modgrid-release-plan.md).
Release gates remain proposed. See [local verification](docs/verification.md)
for checks actually run and [v1 contracts](docs/contracts-v1.md) for supported input.

## What it is

ModGrid treats a modpack as a lockfile: resolve dependencies once, reproduce client and server exactly, explain conflicts, and snapshot saves before updates.

## Planned capability areas

These names describe concerns within the profile/world workflow, not separate
services or a commitment to build a general launcher or server panel. See the
linked release plan for their supported scope and later integration boundaries.

- ModGrid Resolver
- ModGrid Store
- ModGrid Launcher
- ModGrid Server
- ModGrid Network
- ModGrid Security
- ModGrid Monitor
- ModGrid Sync
- ModGrid Console

## First useful slice

Resolve one authored offline Fabric/Minecraft 1.21.1 profile into a deterministic
lockfile, export and run its local server, stop cleanly, and restore its world and
exact profile together into scratch. Verify the restored files before boot and
assert a server-observed world marker after boot.

CloudMesh deployment and PeerVault durable backups follow the independently
working local workflow. See the implementation plan for contract, platform, and
recovery gates and the older PersonalMesh shell's differing Paper acceptance scope.

## Ecosystem relationships

| Relationship | Projects |
|---|---|
| Consumers | PersonalMesh |
| Depends on | C-CAS, P-ANCHOR, E-CLOUDMESH, E-PEERVAULT, P-RESOLVER, P-LICENSE, P-SERVERPACK |
| Shared additions | Resolver, License, ManifestDiff, Crash, Source, ServerPack |

Ecosystems are separate repositories. This repository owns its implementation, tests, releases, and versioning; superplatforms consume it as a pinned dependency.

## Setup, build and test

Install Go **1.27.3**, the pinned toolchain. Run from this repository:

```powershell
go mod download
go build -o bin/ ./cmd/modgrid
go vet ./...
go test ./...
```

The build writes `bin/modgrid.exe` on Windows and `bin/modgrid` on Linux.
Dependency download requires network access once; the built CLI resolves and
verifies offline. Go and the canonical JSON library are pinned in `go.mod` and
`go.sum`. No Java installation is needed for the resolver.

## Offline resolver demo

```powershell
go run ./cmd/modgrid catalog id --catalog tests/fixtures/offline/catalog.json
go run ./cmd/modgrid resolve --pack tests/fixtures/offline/pack.json --catalog tests/fixtures/offline/catalog.json --out out/modgrid.lock.json
go run ./cmd/modgrid lock verify --lock out/modgrid.lock.json --pack tests/fixtures/offline/pack.json --catalog tests/fixtures/offline/catalog.json --expected-id sha256:9aad197a91af8eba3b653260c9efd464ea5e2b1f467fb107f5029f32dded4ba5
```

The selected server set is `marker 1.0.0`, `helper 1.5.0`, `cosmetics 1.0.0`
and `servertools 1.0.0`. Marker 2.0.0 cannot satisfy its dependency; the resolver
backtracks. The lock records the satisfiable helper/marker cycle, included
cosmetics, excluded telemetry and client-only HUD root. Add `--json` to resolve
or verify for a machine-readable report containing the exact choices and reasons.
Errors go to stderr as JSON with a stable code, message and reason chain.

The PackSpec pins the normalized catalog SHA-256, so source changes require an
explicit new pin. Lock identity is SHA-256 over RFC 8785 canonical bytes, with no
trailing newline; artifact hashes use SHA-512. Verification requires the original
PackSpec/catalog in this slice and checks the full deterministic result plus an
optional separately retained lock ID. It does not read or verify artifact bytes.

The [fixture](tests/fixtures/offline/README.md) is synthetic authored metadata
and text payloads, with hand-written choices/reasons and a retained golden lock.
It declares the planned Fabric 0.19.5 tuple solely to exercise equality checks;
its text files are not Minecraft mods or evidence of runtime compatibility.

## Full local workflow (planned)

The first demo will resolve the pinned fixture, export a server directory, start
and cleanly stop the exact server, checkpoint, restore into an empty scratch
directory, verify all supported files, and boot to assert the saved world marker.

Cache/export and local checkpoint commands are also available through the CLI.
Their filesystem tests use explicitly synthetic lifecycle results. Server work requires
captured official loader metadata, an exact Java 21 distribution and a real boot
test. The full workflow and Windows/Linux runtime qualification remain open.

## Non-goals

- Redistributing mod files without permission
- A full launcher UI in V0
- Public server hosting at scale
- Cheat clients

## Planning and research

- [Ecosystem design](https://github.com/jkalend/project-planning/blob/main/projects/modgrid.md)
- [PersonalMesh](https://github.com/jkalend/project-planning/blob/main/platforms/personalmesh.md)
- [Additions catalog](https://github.com/jkalend/project-planning/blob/main/components/additions-catalog.md)
- [PersonalMesh research](https://github.com/jkalend/project-planning/blob/main/research/personalmesh-research.md)
- [Confidence assessment](https://github.com/jkalend/project-planning/blob/main/research/research-quality-2026-10-18.md)

## License

Private. All rights reserved until a license is chosen.

Public source: [jkalend/ModGrid](https://github.com/jkalend/ModGrid). Public
visibility does not change the [license](LICENSE). CI checks both Windows and
Linux, runs race/fuzz tests, and scans dependencies. The full 1.0 acceptance
matrix remains open while implementation and runtime qualification continue.
