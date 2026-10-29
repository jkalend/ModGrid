# ModGrid agent guide

## Start here

Read [README.md](README.md) and, when present, `../AGENTS.md` for workspace conventions. For feature or architecture work, read the [canonical design](https://github.com/jkalend/project-planning/blob/main/projects/modgrid.md), available locally at `../../project-planning/projects/modgrid.md`.

The Go CLI implements offline authored-fixture resolution and canonical lock verification. Export, launcher, server lifecycle and recovery remain planned. Read [the v1 contract](docs/contracts-v1.md) before changing persisted inputs, ordering or lock identity; use the README for runnable commands.

## Scope and first slice

ModGrid owns deterministic modpack resolution, client/server manifests, server export/lifecycle, and save-safe updates. CloudMesh owns host deployment; PeerVault owns durable snapshots and restore.

Start with `PackSpec` → deterministic lockfile → local server pack/start. Then connect deployment and world snapshot/restore through CloudMesh/PeerVault adapters. A working local resolver/exporter should not require the rest of PersonalMesh.

## Design invariants

- Lock exact Minecraft/loader/mod versions, hashes, dependency choices, and client/server side. Define stable tie-breaking and canonical lockfile serialization; identical metadata inputs must resolve identically.
- Pin metadata inputs or record source revisions so upstream changes can explain a changed result. Conflicts need actionable unsatisfied constraints, not arbitrary dependency removal.
- Verify downloaded bytes against locked hashes and allowed sources. Treat metadata, filenames, and archives as untrusted; reject traversal, unsafe extraction paths, and decompression bombs.
- Export from the lockfile without opportunistic upgrades. Check client/server compatibility and required dependencies while allowing explicitly client-only mods to remain client-only.
- Retain source/license metadata and honor redistribution permissions. Where redistribution is unavailable, emit authorized download references instead of bundling mod files.
- Before an update, quiesce world writes or stop the server, create and verify a restorable snapshot, then stage the new pack. Restore must pair the world with its compatible pack/lockfile; preserve the original save when testing recovery.
- Keep the scope on resolution and dedicated servers. Reuse launcher integrations rather than building a general launcher UI in V0.

## Validation priorities

- Use pinned synthetic metadata fixtures to test deterministic resolution, dependency cycles, incompatible loaders, side constraints, missing artifacts, and unsatisfiable explanations.
- The same lockfile yields the same artifact hashes despite metadata ordering or cache state. Reject corrupt downloads and unsafe archives.
- Exercise server start/stop/crash and save-quiesce failure with disposable worlds; never use a personal world as a fixture.
- Prove update rollback restores both pack identity and world data. Label simulated CloudMesh/PeerVault integration explicitly.

## Working and completion

Run Git and tooling from this repository and preserve unrelated changes. Discover commands from real manifests/scripts; document loader/Java prerequisites and runnable build/test/demo commands with the first slice. Keep mod binaries, credentials, and real worlds outside Git. Report checks actually run, fixture metadata identity, and any untested live deployment/restore path.
