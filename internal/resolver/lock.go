package resolver

import (
	"bytes"
	"encoding/json"
)

// ReadLock checks canonical bytes and selected graph facts without the original
// catalog or choosing any artifact outside the lock.
func ReadLock(b []byte, expectedID string) (Lock, string, error) {
	var lock Lock
	if err := decode(b, &lock); err != nil {
		return lock, "", err
	}
	if lock.Schema != "modgrid.lock.v1" {
		return lock, "", failure("UnsupportedSchema", "expected modgrid.lock.v1")
	}
	id := digest(b)
	if expectedID != "" && (!sha256Pattern.MatchString(expectedID) || expectedID != id) {
		return lock, "", failure("LockMismatch", "lock identity does not match expected SHA-256")
	}
	if !sha256Pattern.MatchString(lock.Pack.CatalogSHA256) {
		return lock, "", failure("InvalidInput", "invalid frozen catalog identity")
	}
	seen := map[string]bool{}
	for _, a := range lock.Artifacts {
		if seen[a.Project] {
			return lock, "", failure("DuplicateID", "multiple locked files for "+a.Project)
		}
		seen[a.Project] = true
	}
	c := Catalog{Schema: "modgrid.catalog.v1", ID: lock.Pack.CatalogID, Artifacts: lock.Artifacts}
	cb, err := json.Marshal(c)
	if err != nil {
		return lock, "", err
	}
	ch, err := CatalogIdentity(cb)
	if err != nil {
		return lock, "", err
	}
	p := lock.Pack
	p.CatalogSHA256 = ch
	pb, err := json.Marshal(p)
	if err != nil {
		return lock, "", err
	}
	expected, _, _, err := Resolve(pb, cb)
	if err != nil {
		return lock, "", err
	}
	expected.Pack.CatalogSHA256 = lock.Pack.CatalogSHA256
	eb, err := canonical(expected)
	if err != nil {
		return lock, "", err
	}
	if !bytes.Equal(b, eb) {
		return lock, "", failure("LockMismatch", "lock is noncanonical or contains invalid choices, closure or reasons")
	}
	return lock, id, nil
}
