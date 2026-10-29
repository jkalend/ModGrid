# Authored offline resolver fixture v1

All metadata, golden expectations and text payloads in this directory are
project-authored test material under the private repository license.
`LicenseRef-ModGrid-Private` records that local fixture policy; it grants no
redistribution permission. No Minecraft, Fabric, provider data or real mod
binary is included.

`offline-v1` normalized catalog identity:
`sha256:ac3d2d3157e9a6ccc26234eb10dab42a071d1628d99b9cb414fdb23dfaa8a6f5`.
`pack.json` pins it and targets a synthetic server tuple. `expected.lock.json`
retains the exact canonical bytes; `expected.lock.id` retains their SHA-256.

Hand-written selection/reason oracles are in `TestAuthoredOracleAndGoldenLock`.
Expected server choices are cosmetics 1.0.0, helper 1.5.0, marker 1.0.0 and
servertools 1.0.0. Marker 2 requires nonexistent helper >=2; marker 1 requires
helper >=1,<2, chooses the highest eligible 1.5, and helper 1.5 requires marker
>=1,<2. Cosmetics is explicitly included; telemetry explicitly excluded.
Client HUD applies only to the client; server tools only to the server. The
client test includes telemetry and checks the changed side-specific set.

Artifact hashes and sizes refer to `payloads/<file_id>.txt`, whose exact bytes
are UTF-8 `ModGrid authored text fixture: <file_id>` followed by LF. They are
plain text, not loadable mods. LF checkout rules are identity-critical.
The metadata's Fabric 0.19.5 / Minecraft 1.21.1 declaration tests tuple equality
and has no official compatibility evidence. Actual loader/runtime pins and a
Java marker mod must be acquired/built/boot-tested before S4.

The tests mutate these base objects to exercise malformed input, duplicate IDs,
blocked rights, missing dependencies, incompatible tuples, unknown sides,
unsupported ranges, side conflicts, optional choices, contradictory cycles,
source tie-breaking, budgets and tampering. Mutations are pinned to a newly
computed catalog digest unless the test specifically checks pin mismatch.

To intentionally change the fixture, review the hand-written oracle, update the
catalog digest in PackSpec using `catalog id`, resolve into a temporary lock and
review its choices/reasons/bytes before replacing the golden lock and ID. Tests
never regenerate expected files automatically. This fixture is not a real
server or an ecosystem integration simulation.
