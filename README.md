# ModGrid

> Reproducible modpack resolution and dedicated Minecraft server orchestration.

**Status: planning. No implementation yet.**

Canonical design: [modgrid](https://github.com/jkalend/project-planning/blob/main/projects/modgrid.md)

## What it is

ModGrid treats a modpack as a lockfile: resolve dependencies once, reproduce client and server exactly, explain conflicts, and snapshot saves before updates.

## Subprojects

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

Resolve a pack into a deterministic lockfile, export a server pack, deploy it through CloudMesh, and restore a world snapshot.

## Ecosystem relationships

| Relationship | Projects |
|---|---|
| Consumers | PersonalMesh |
| Depends on | C-CAS, P-ANCHOR, E-CLOUDMESH, E-PEERVAULT, P-RESOLVER, P-LICENSE, P-SERVERPACK |
| Shared additions | Resolver, License, ManifestDiff, Crash, Source, ServerPack |

Ecosystems are separate repositories. This repository owns its implementation, tests, releases, and versioning; superplatforms consume it as a pinned dependency.

## Planned repository layout

```text
ModGrid/
├─ cmd/
├─ internal/resolver/
├─ internal/server/
├─ manifests/
├─ tests/
├─ docs/
└─ README.md
```

## First demo (planned)

```bash
modgrid resolve pack.yaml --lock modgrid.lock
modgrid server export --lock modgrid.lock --out server-pack/
modgrid deploy server-pack --host homelab-1
modgrid snapshot --wait
```

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
