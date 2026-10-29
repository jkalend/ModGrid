# Offline resolver contracts v1

Implemented S0/S1 subset of the [canonical design](https://github.com/jkalend/project-planning/blob/main/projects/modgrid.md).
The full field definitions are the JSON-tagged types in
[`internal/resolver/model.go`](../internal/resolver/model.go), with readable
[`pack.json`](../tests/fixtures/offline/pack.json) and
[`catalog.json`](../tests/fixtures/offline/catalog.json) examples. These are authored
fixture schemas, not parsers for Fabric, Modrinth or another provider's metadata.
RuntimeProfile, export, checkpoint and recovery contracts are not implemented.

## Parsing and budgets

One UTF-8 JSON object per document, at most 4 MiB. Every declared field is required,
case sensitive, and must have its exact type. Unknown fields, duplicate object
keys, null, trailing documents, invalid UTF-8 and unpaired UTF-16 escapes fail.
Empty arrays and empty license labels are explicit; other evidence text must be
nonempty and have no control characters. Schemas must exactly match the supported
v1 strings. Changing a schema's meaning requires a new schema version and an
explicit migration; no best-effort version conversion exists.

| Bound | v1 ceiling |
|---|---:|
| JSON nesting | 32 |
| Decoded string value | 2,048 UTF-8 bytes |
| Numeric representation | decimal integer token, 0 through 2^53−1; no exponent/fraction/sign |
| Catalog artifacts / distinct artifact projects | 256 / 64 |
| Total catalog dependency edges | 1,024 |
| Pack roots | 1 through 64 |
| Optional choices / config files | 64 / 64 |
| Artifact declared size | 0 through 268,435,456 bytes |
| Version component | 0 through 2^32−1 |
| Constraint comparison terms | 8 |
| Candidate decisions per solve | 10,000 |

The engine has bounded inputs and a deterministic decision budget; budget
exhaustion is `ResourceLimit`, never `Unsatisfiable`. No wall-clock or power-loss
guarantee is made. There are no cache or network calls in the engine. Local input
files and output parent directories are under the operator's control; this CLI
does not defend against a concurrent malicious filesystem writer.

## Metadata identity

Identifiers use `[a-z0-9][a-z0-9._-]{0,63}`. File IDs are globally unique within
the catalog; multiple files may share a source/project/version ID only if their
numeric version agrees. Each artifact supplies exact project/version/file IDs,
source ID/revision/original facts, size and tagged lowercase SHA-512, exact game
and loader tuple plus evidence, original and normalized sides, dependencies,
license label, and separate rights status/evidence. `license` may be empty when
no label exists. A matching hash or a license label grants no permission.

Dependency edges retain their authored original expression alongside the
normalized range. Duplicate dependency project IDs within an artifact are input
errors. The catalog is validated in full, including candidates not selected, so
unknown sides or unsupported syntax cannot silently cause a version downgrade.

Catalog ID is the authored name. Catalog content identity is tagged SHA-256 of
the normalized catalog's RFC 8785 JCS bytes: artifacts sorted by file ID,
dependencies by project, comparison terms sorted lexically and deduplicated.
All original fact/evidence strings remain identity-bearing. Array order and JSON
formatting do not change this digest; source-fact changes do. `catalog id` reports
this digest for review and explicit insertion into `catalog_sha256` in PackSpec.
A solve requires both the exact catalog name and this content identity.

## PackSpec and resolution

PackSpec has schema `modgrid.pack.v1`, stable profile ID, catalog name/digest,
target, roots, optional decisions and config hashes. Target supports the authored
Minecraft `1.21.1` profile, loader ID `fabric` with exact numeric version, Java
major `21`, platform `windows/amd64` or `linux/amd64`, and `client` or `server`.
This is metadata equality, not validation that a real server supports the tuple.

`FixtureVersionV1` accepts three nonnegative numeric components without leading
zeroes. Constraints are a bare exact version, an `=`, `<`, `<=`, `>` or `>=`
comparison immediately followed by a version, or comma-separated conjunctions
of explicit comparisons. ASCII whitespace around terms is allowed. Reject
prereleases, wildcards, unions, caret/tilde ranges and arbitrary provider syntax.
Bare exact versions normalize to `=major.minor.patch`; comparison terms sort
lexically and duplicates collapse.

Each unique root project declares its applicable sides. A client-only root is
retained in PackSpec but creates no server demand. Dependency edges apply on
the selected side and cannot require an unsupported-side artifact. Normalized
artifact side values are `required`, `optional`, `unsupported`; both unsupported
is invalid. Unknown/ambiguous side values are input errors.

Choices are unique `{project, include}` pairs. An explicit choice is required for
every optional root/dependency in the catalog and each artifact optional on the
target side, even if a candidate later proves unreachable. This conservative
fixture rule avoids guesses during backtracking. False skips an optional edge
or root; true makes it a demand. Optional-side candidates require true, and
cannot satisfy a required demand when explicitly excluded. Choices are per
project for this single target side, not per version or provider. Config hashes
are identity inputs only; S1 does not read config files.

Config paths use a conservative lowercase ASCII subset of PortablePathV1:
`[a-z0-9._/-]`, at most 180 bytes, 32 components, 120 bytes per component; no empty,
`.`/`..`, trailing dot, Windows reserved base name, absolute path, backslash,
duplicate path, or file/directory prefix collision. Config digests are tagged
lowercase SHA-256. Full archive/path profiles are deferred to export/import.

One artifact per demanded project is selected. Required dependencies, cycles,
and all accumulated ranges must be satisfied. Choose unresolved projects in
lexical project order; candidates in descending numeric version, then lexical
source/project/version-ID/file-ID order. Exact constraints filter candidates
before selection. Artifacts must match target tuple/side and have `local-use`
rights evidence; `unknown` and `blocked` fail that eligibility gate.

Failed branches backtrack in that stable order. A conflict reports its target
side and the active source/file or root requirements, in stable order. It is
neither a shortest explanation nor a minimal unsatisfiable core. Typed errors
include `InvalidInput`, `UnsupportedSchema`, `UnsupportedVersion`,
`UnsupportedConstraint`, `UnsupportedTarget`, `DuplicateID`, `UnknownSide`,
`OptionalChoiceRequired`, `CatalogMismatch`, `MissingArtifact`,
`IncompatibleTarget`, `SideConflict`, `RightsBlocked`, `Unsatisfiable`, and
`ResourceLimit`. Source I/O uses `SourceUnavailable`/`IOError`; CLI syntax uses
`Usage`. Errors do not silently drop required dependencies.

## Lock identity and verification

`modgrid.lock.v1` embeds the normalized PackSpec, selected complete artifacts
sorted by project, and reasons sorted by project/from/constraint. Roots sort by
project, root sides lexically, choices by project and configs by path. A root
reason's `from` is `pack:<id>`; a dependency reason's `from` is
`<source>/<project>/<version_id>/<file_id>`. Empty arrays remain arrays.

Lock bytes are [RFC 8785 JCS](https://www.rfc-editor.org/rfc/rfc8785.html), without
BOM, padding or trailing newline. Lock ID is `sha256:<lowercase hex>` of these
exact bytes, retained separately to avoid self-reference. Local paths, clocks,
secrets and mutable lookups have no lock fields. Artifact content integrity is
separately `sha512:<lowercase hex>`. Neither identity proves trust, rights,
runtime compatibility or world compatibility.

S1 `lock verify` strictly parses the lock, optionally compares an externally
retained ID, solves the supplied pinned PackSpec/catalog again, and compares every
byte to the canonical expected lock. It detects altered choices/reasons, schema
fields, formatting and identity. Without an external expected ID it establishes
consistency with the supplied inputs, not independent authenticity. It does not
read artifacts, config files or resolve a live source. Future S2 replay must
consume the lock's exact choices without solving again.

Only a successful solve reaches publication. The CLI refuses a nonregular output
and overwriting an input file, writes/syncs/closes a unique temporary sibling,
then renames it into place. Failure before rename preserves the old lock and
attempts temporary cleanup. This is ordinary local file replacement, with no
instance selector, world transaction, concurrency fencing or crash/power-loss
durability claim. `--json` reports have schema `modgrid.report.v1`; diagnostics
are `{error:{code,message,reasons}}` on stderr with exit code 1.
