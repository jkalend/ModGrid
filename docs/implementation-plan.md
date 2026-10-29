# ModGrid implementation and release roadmap

Status: early implementation, 2026-10-29. The Go CLI implements the S1 offline
resolver and lock verifier, with S0 resolver contracts and authored fixtures.
Windows checks are recorded in [verification](verification.md). S0 real runtime
pins, Linux execution and S2–S7 remain open; no release gate is claimed complete.

This is the short delivery map. Detailed contracts, fixtures, failure protocols,
release gates and source evidence live in **project-planning**:

- [Canonical ModGrid design](https://github.com/jkalend/project-planning/blob/main/projects/modgrid.md)
  — scope, invariants, supported profile, archive limits and recovery protocol.
- [Detailed delivery and full-release plan](https://github.com/jkalend/project-planning/blob/main/projects/modgrid-release-plan.md)
  — S0–S7 implementation detail, later product stages, integration gates and maintenance.
- [Evidence dossier](https://github.com/jkalend/project-planning/blob/main/research/ecosystems/modgrid.md)
  and [research closure](https://github.com/jkalend/project-planning/blob/main/research/research-expansion-2026-10-22/ecosystem-modgrid.md)
  — provider/runtime findings, references and unrun validation.

The corresponding local files are under `../../project-planning/` from the
ModGrid repository root. Links target that repository's GitHub main branch;
uncommitted local additions will become available there after publication.
This plan does not claim they have been pushed.

## V0: prove the local workflow

Resolve one authored offline Fabric/Minecraft 1.21.1 profile, export and run its
server, stop cleanly, checkpoint the exact world/pack/runtime/config together,
restore to scratch, verify every supported file before boot and assert a
server-observed world marker. Keep the original world and checkpoint intact when
testing a failed update or restore.

The first slice uses one Go module/CLI; see the [toolchain ADR](decisions/0001-offline-resolver.md).
The authored Java fixture remains future work. Exact runtime/loader pins must
be captured and boot-tested; Fabric Loader 0.19.5 remains a historical candidate.
Qualify named Linux and Windows local-filesystem profiles before claiming V0.

| Slice | Deliverable and exit gate |
|---|---|
| [S0 — Contracts and fixture](https://github.com/jkalend/project-planning/blob/main/projects/modgrid-release-plan.md#s0--freeze-the-executable-fixture-and-local-contracts) | Pin toolchain/runtime/fixture; freeze schemas, budgets, stable ordering and hand-written oracles. |
| [S1 — Offline resolver](https://github.com/jkalend/project-planning/blob/main/projects/modgrid-release-plan.md#s1--offline-packspec-to-deterministic-lock-v01) | PackSpec/catalog → validated canonical lock and reasons; repeated/shuffled solves agree (V0.1). |
| [S2 — Replay/export](https://github.com/jkalend/project-planning/blob/main/projects/modgrid-release-plan.md#s2--exact-artifact-replay-and-fresh-server-directory-export) | Exact local bytes → fresh side-correct server tree; corrupt/missing inputs cannot mutate an existing destination. |
| [S3 — Checkpoint authority](https://github.com/jkalend/project-planning/blob/main/projects/modgrid-release-plan.md#s3--immutable-generations-and-one-authoritative-selector) | Immutable verified generations, one lifecycle lock and selector; faults yield a valid old/new target or explicit recovery. |
| [S4 — Real server lifecycle](https://github.com/jkalend/project-planning/blob/main/projects/modgrid-release-plan.md#s4--local-fabric-lifecycle-and-server-observed-marker-v02) | Explicit EULA/start, direct foreground Java, single writer, status, marker and confirmed clean stop (V0.2). |
| [S5 — Recovery/rollback](https://github.com/jkalend/project-planning/blob/main/projects/modgrid-release-plan.md#s5--stop-first-checkpoint-scratch-restore-and-paired-rollback-v03-path) | Full pre-boot tree verification plus restored marker; failed candidate preserves the old world/profile. |
| [S6 — Authored .mrpack](https://github.com/jkalend/project-planning/blob/main/projects/modgrid-release-plan.md#s6--bounded-authored-mrpack-compatibility) | Bounded formatVersion 1 import, safe paths/overlays, both required hashes, explicit side/optional semantics. |
| [S7 — V0 qualification](https://github.com/jkalend/project-planning/blob/main/projects/modgrid-release-plan.md#s7--retention-concurrency-and-release-qualification) | Protected GC/pin/lease races, transaction fault matrix and clean-host runbooks pass on both declared OS/filesystem profiles (complete V0.3). |

Order: S0 → S1 → S2 → S3 → S4 → S5 → S6 → S7. **S0/S1 resolver work is implemented**;
the README contains build/test/demo commands. Continue with Linux resolver
qualification, the remaining runtime fixture work, then S2 exact artifact export.
Local checkpoints are not an independent/offsite backup, and process-kill tests
cannot establish power-loss durability.

## Full standalone release

V0 is the authored proof. The proposed 1.0 product is the complete owner workflow:
maintain named permitted client/server profiles, explain exact choices, acquire
supported inputs, review updates and recover the matching world/profile using
supported release artifacts. The destination stays local and self-hostable.

| Stage after V0 | Result and release gate |
|---|---|
| [R1 — Profiles and clients](https://github.com/jkalend/project-planning/blob/main/projects/modgrid-release-plan.md#r1--complete-named-profiles-and-the-clientserver-workflow) | Stable profile ownership/inspect/import/export, real supported Fabric metadata grammar, user-edit handling; a named existing launcher imports the client pack and joins the matching disposable server. |
| [R2 — Provider acquisition](https://github.com/jkalend/project-planning/blob/main/projects/modgrid-release-plan.md#r2--one-provider-backed-acquisition-path) | One bounded Modrinth API path for the reviewed local personal/non-commercial profile; source/rights/rate/redirect/hash failures tested; captured locks replay offline. |
| [R3 — Controlled updates](https://github.com/jkalend/project-planning/blob/main/projects/modgrid-release-plan.md#r3--review-and-promote-controlled-updates) | Review source/dependency/side/config/runtime/rights diff, checkpoint, validate an isolated candidate, explicitly promote; rollback restores old world and pack together. |
| [R4 — Operational beta](https://github.com/jkalend/project-planning/blob/main/projects/modgrid-release-plan.md#r4--operational-beta-and-recovery-readiness) | Complete operator/failure runbooks, exact support matrix, clean-host recovery and repeated update/restore use; no unresolved unsafe-write or data-loss blocker. |
| [R5 — RC and 1.0](https://github.com/jkalend/project-planning/blob/main/projects/modgrid-release-plan.md#r5--release-candidate-and-standalone-10) | Frozen scope, compatibility/migration tests, clean packaged smoke, license/notices/SBOM, checksums/publisher verification, evidence manifest and owner go/no-go; publish and verify the qualified artifact set. |
| [After 1.0](https://github.com/jkalend/project-planning/blob/main/projects/modgrid-release-plan.md#after-10) | Maintain supported versions/security, rehearse restores, qualify patches/upgrades and add another loader/profile only with an independent oracle. |

The detailed plan maps Resolver, Store, Launcher, Server, Network, Security,
Monitor, Sync and Console concerns to this workflow. They are not separate
services to scaffold. A custom launcher/server panel, hosted catalog, world sync,
automatic upgrades, public ingress and cross-version world conversion are not
implicit 1.0 promises.

## Integration releases

The [optional integration tracks](https://github.com/jkalend/project-planning/blob/main/projects/modgrid-release-plan.md#optional-integration-release-tracks)
follow working local recovery and each participating owner's released contract:

| Track | Owner and required proof |
|---|---|
| PeerVault | [PeerVault plan](https://github.com/jkalend/project-planning/blob/main/projects/peervault.md): durable storage/key recovery; clean-host full restore plus ModGrid's exact profile/world oracle. |
| CloudMesh | [CloudMesh plan](https://github.com/jkalend/project-planning/blob/main/projects/cloudmesh.md): host deployment/reconciliation; one lifecycle authority, idempotency and lost-outcome/no-duplicate-writer tests. |
| PersonalMesh | [PersonalMesh plan](https://github.com/jkalend/project-planning/blob/main/platforms/personalmesh.md): pinned ecosystem artifacts and shell-owned end-to-end acceptance after scope alignment. |

These integrations are required only for a release advertising that profile;
standalone resolve/replay/restore must remain usable without them. The older
PersonalMesh shell still names Paper/agent-update rollback, and its canonical
plan retains stale lock-algorithm wording. Reconcile those owner documents before
cross-project promotion; ModGrid uses SHA-256 over canonical lock bytes and
SHA-512 for artifact integrity. Simulated adapters do not establish integration.

Follow [D-0005](https://github.com/jkalend/project-planning/blob/main/decisions/0005-repository-layout.md)
for repository and release ownership, [D-0002](https://github.com/jkalend/project-planning/blob/main/decisions/0002-first-foundation.md)
for the qualified CAS direction, and [ROADMAP gates](https://github.com/jkalend/project-planning/blob/main/ROADMAP.md#decision-gates)
for clean-machine/restore/stability evidence. Only the checks explicitly recorded
in local verification have run; all later gates remain proposed.
