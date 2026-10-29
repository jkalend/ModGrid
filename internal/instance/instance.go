// Package instance owns immutable paired pack/world generations and one durable
// selector, protected by the same OS lifecycle lock for every state operation.
package instance

import (
	"context"
	"errors"
	"modgrid.local/modgrid/internal/document"
	"modgrid.local/modgrid/internal/localfs"
	"modgrid.local/modgrid/internal/pack"
	"modgrid.local/modgrid/internal/resolver"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

type Lease struct {
	ID         string `json:"id"`
	Generation string `json:"generation"`
	Kind       string `json:"kind"`
}
type Working struct {
	ID         string `json:"id"`
	Generation string `json:"generation"`
	State      string `json:"state"`
	Validated  bool   `json:"application_validated"`
}
type Body struct {
	Schema   string   `json:"schema"`
	Sequence int64    `json:"sequence"`
	Active   string   `json:"active"`
	Previous string   `json:"previous"`
	Pins     []string `json:"pins"`
	Leases   []Lease  `json:"leases"`
	Work     Working  `json:"working"`
}
type Selection struct {
	Body     Body   `json:"body"`
	Checksum string `json:"checksum"`
}
type Generation struct {
	Schema               string           `json:"schema"`
	ID                   string           `json:"id"`
	LockID               string           `json:"lock_id"`
	RuntimeID            string           `json:"runtime_id"`
	ApplicationValidated bool             `json:"application_validated"`
	Files                localfs.Manifest `json:"files"`
}
type Completion struct {
	Schema         string `json:"schema"`
	Generation     string `json:"generation"`
	ManifestSHA256 string `json:"manifest_sha256"`
}

// Fault is an internal deterministic test seam; production callers leave it nil.
type Store struct {
	Root  string
	Fault func(string) error
}

func validID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
func (s Store) point(name string) error {
	if s.Fault != nil {
		return s.Fault(name)
	}
	return nil
}
func (s Store) genPath(id string) string     { return filepath.Join(s.Root, "generations", id) }
func (s Store) workingPath(id string) string { return filepath.Join(s.Root, "working", id) }

func (s Store) Initialize(ctx context.Context, payload string) (Selection, error) {
	var zero Selection
	if err := os.Mkdir(s.Root, 0700); err != nil {
		return zero, document.Fail("DestinationExists", "initialize requires an absent instance root: "+err.Error())
	}
	for _, name := range []string{"generations", "working", "stages"} {
		if err := os.Mkdir(filepath.Join(s.Root, name), 0700); err != nil {
			return zero, err
		}
	}
	release, err := localfs.Lock(ctx, filepath.Join(s.Root, "lifecycle.lock"))
	if err != nil {
		return zero, err
	}
	defer release()
	g, err := s.stage(ctx, payload, false)
	if err != nil {
		return zero, err
	}
	body := Body{Schema: "modgrid.selection.v1", Sequence: 1, Active: g.ID, Pins: []string{}, Leases: []Lease{}, Work: Working{State: "none"}}
	return s.commit(body)
}

func (s Store) Inspect(ctx context.Context) (Selection, error) {
	var result Selection
	err := s.with(ctx, func(current Selection) error { result = current; return nil })
	return result, err
}

func (s Store) with(ctx context.Context, fn func(Selection) error) error {
	release, err := localfs.Lock(ctx, filepath.Join(s.Root, "lifecycle.lock"))
	if err != nil {
		return err
	}
	defer release()
	current, err := s.read(ctx)
	if err != nil {
		return err
	}
	return fn(current)
}

func (s Store) read(ctx context.Context) (Selection, error) {
	var sel Selection
	b, err := localfs.Read(filepath.Join(s.Root, "selection.json"))
	if err != nil {
		return sel, document.Fail("RecoveryRequired", "missing/unreadable selector; no generation is auto-selected")
	}
	if err := document.Decode(b, &sel); err != nil {
		return sel, document.Fail("RecoveryRequired", err.Error())
	}
	canonical, err := document.Canonical(sel.Body)
	if err != nil {
		return sel, err
	}
	if sel.Body.Schema != "modgrid.selection.v1" || sel.Body.Sequence < 1 || sel.Body.Sequence >= 9007199254740991 || document.Digest(canonical) != sel.Checksum {
		return sel, document.Fail("RecoveryRequired", "invalid selector version, sequence or checksum")
	}
	if !validID(sel.Body.Active) || (sel.Body.Previous != "" && !validID(sel.Body.Previous)) || len(sel.Body.Pins) > 64 || len(sel.Body.Leases) > 64 {
		return sel, document.Fail("RecoveryRequired", "invalid generation references")
	}
	if !sort.StringsAreSorted(sel.Body.Pins) || len(slices.Compact(slices.Clone(sel.Body.Pins))) != len(sel.Body.Pins) {
		return sel, document.Fail("RecoveryRequired", "noncanonical pins")
	}
	refs := []string{sel.Body.Active}
	if sel.Body.Previous != "" {
		refs = append(refs, sel.Body.Previous)
	}
	refs = append(refs, sel.Body.Pins...)
	leaseIDs := map[string]bool{}
	last := ""
	for _, l := range sel.Body.Leases {
		if !validID(l.ID) || !validID(l.Generation) || (l.Kind != "server" && l.Kind != "restore") || leaseIDs[l.ID] || l.ID < last {
			return sel, document.Fail("RecoveryRequired", "invalid lease table")
		}
		leaseIDs[l.ID] = true
		last = l.ID
		refs = append(refs, l.Generation)
	}
	w := sel.Body.Work
	if !slices.Contains([]string{"none", "running", "clean", "faulted"}, w.State) {
		return sel, document.Fail("RecoveryRequired", "unknown working state")
	}
	if w.State == "none" {
		if w.ID != "" || w.Generation != "" || w.Validated {
			return sel, document.Fail("RecoveryRequired", "unexpected working reference")
		}
	} else {
		if !validID(w.ID) || !validID(w.Generation) {
			return sel, document.Fail("RecoveryRequired", "invalid working reference")
		}
		refs = append(refs, w.Generation)
		if w.State == "running" && !leaseIDs[w.ID] {
			return sel, document.Fail("RecoveryRequired", "running working copy has no durable lease")
		}
	}
	for _, id := range refs {
		if !validID(id) {
			return sel, document.Fail("RecoveryRequired", "invalid reference")
		}
		if _, err := s.verifyGeneration(ctx, id); err != nil {
			return sel, document.Fail("RecoveryRequired", err.Error())
		}
	}
	return sel, nil
}

func (s Store) commit(body Body) (Selection, error) {
	sort.Strings(body.Pins)
	sort.Slice(body.Leases, func(i, j int) bool { return body.Leases[i].ID < body.Leases[j].ID })
	b, err := document.Canonical(body)
	if err != nil {
		return Selection{}, err
	}
	sel := Selection{Body: body, Checksum: document.Digest(b)}
	if err := s.point("before-selector"); err != nil {
		return sel, err
	}
	if err := localfs.WriteJSON(filepath.Join(s.Root, "selection.json"), sel, true); err != nil {
		return sel, document.Fail("CommitUnknown", "selector write/flush failed; inspect before retry: "+err.Error())
	}
	if err := s.point("after-selector"); err != nil {
		return sel, document.Fail("CommitUnknown", err.Error())
	}
	return sel, nil
}

func (s Store) stage(ctx context.Context, source string, validated bool) (Generation, error) {
	var g Generation
	files, err := localfs.Scan(ctx, source)
	if err != nil {
		return g, err
	}
	id := localfs.ID()
	stage := filepath.Join(s.Root, "stages", id)
	if err := os.Mkdir(stage, 0700); err != nil {
		return g, err
	}
	// Interrupted stages are retained as unselectable recovery material.
	payload := filepath.Join(stage, "payload")
	if err := os.Mkdir(payload, 0700); err != nil {
		return g, err
	}
	if err := localfs.CopyInto(ctx, source, payload, files); err != nil {
		return g, err
	}
	if err := s.point("after-data"); err != nil {
		return g, err
	}
	lockBytes, err := localfs.Read(filepath.Join(payload, "modgrid.lock.json"))
	if err != nil {
		return g, err
	}
	_, lockID, err := resolver.ReadLock(lockBytes, "")
	if err != nil {
		return g, err
	}
	if err := pack.VerifyPayload(ctx, payload, lockBytes); err != nil {
		return g, err
	}
	runtimeID := ""
	if runtimeBytes, err := localfs.Read(filepath.Join(payload, "modgrid.runtime.json")); err == nil {
		runtimeID = document.Digest(runtimeBytes)
	} else if !errors.Is(err, os.ErrNotExist) {
		return g, err
	}
	g = Generation{Schema: "modgrid.generation.v1", ID: id, LockID: lockID, RuntimeID: runtimeID, ApplicationValidated: validated, Files: files}
	b, err := document.Canonical(g)
	if err != nil {
		return g, err
	}
	if err := localfs.Write(filepath.Join(stage, "generation.json"), b, false); err != nil {
		return g, err
	}
	if err := s.point("after-manifest"); err != nil {
		return g, err
	}
	marker := Completion{Schema: "modgrid.complete.v1", Generation: id, ManifestSHA256: document.Digest(b)}
	if err := localfs.WriteJSON(filepath.Join(stage, "complete.json"), marker, false); err != nil {
		return g, err
	}
	if err := s.point("after-marker"); err != nil {
		return g, err
	}
	if err := localfs.SyncTree(stage); err != nil {
		return g, err
	}
	if err := localfs.Rename(stage, s.genPath(id), false); err != nil {
		return g, err
	}
	if err := localfs.SyncDir(filepath.Join(s.Root, "stages")); err != nil {
		return g, err
	}
	if err := localfs.SyncDir(filepath.Join(s.Root, "generations")); err != nil {
		return g, err
	}
	if err := s.point("after-install"); err != nil {
		return g, err
	}
	_, err = s.verifyGeneration(ctx, id)
	return g, err
}

func (s Store) verifyGeneration(ctx context.Context, id string) (Generation, error) {
	var g Generation
	var complete Completion
	if !validID(id) {
		return g, document.Fail("RecoveryRequired", "invalid generation ID")
	}
	root := s.genPath(id)
	b, err := localfs.Read(filepath.Join(root, "generation.json"))
	if err != nil {
		return g, err
	}
	if err := document.Decode(b, &g); err != nil {
		return g, err
	}
	cb, err := localfs.Read(filepath.Join(root, "complete.json"))
	if err != nil {
		return g, err
	}
	if err := document.Decode(cb, &complete); err != nil {
		return g, err
	}
	if g.Schema != "modgrid.generation.v1" || g.ID != id || complete.Schema != "modgrid.complete.v1" || complete.Generation != id || complete.ManifestSHA256 != document.Digest(b) {
		return g, document.Fail("RecoveryRequired", "generation completion/manifest mismatch")
	}
	if err := localfs.Verify(ctx, filepath.Join(root, "payload"), g.Files); err != nil {
		return g, err
	}
	lock, err := localfs.Read(filepath.Join(root, "payload", "modgrid.lock.json"))
	if err != nil {
		return g, err
	}
	if _, _, err := resolver.ReadLock(lock, g.LockID); err != nil {
		return g, err
	}
	if g.RuntimeID != "" {
		rb, err := localfs.Read(filepath.Join(root, "payload", "modgrid.runtime.json"))
		if err != nil {
			return g, err
		}
		if document.Digest(rb) != g.RuntimeID {
			return g, document.Fail("RecoveryRequired", "runtime identity mismatch")
		}
	}
	return g, nil
}

func sequence(current Selection, expected int64) error {
	if current.Body.Sequence != expected {
		return document.Fail("StaleSequence", "selector changed; inspect and review again")
	}
	return nil
}

func (s Store) Checkpoint(ctx context.Context, expected int64) (Selection, error) {
	var result Selection
	err := s.with(ctx, func(current Selection) error {
		if err := sequence(current, expected); err != nil {
			return err
		}
		if current.Body.Work.State != "clean" {
			return document.Fail("UnconfirmedStop", "checkpoint requires a managed, confirmed clean stop")
		}
		g, err := s.stage(ctx, s.workingPath(current.Body.Work.ID), current.Body.Work.Validated)
		if err != nil {
			return err
		}
		body := current.Body
		body.Sequence++
		body.Previous = body.Active
		body.Active = g.ID
		body.Work = Working{State: "none"}
		result, err = s.commit(body)
		return err
	})
	return result, err
}

// Restore copies into a new empty target and fully verifies before returning.
// A durable lease is retained if copying or final lease release fails.
func (s Store) Restore(ctx context.Context, id, destination string, expected int64) (localfs.Manifest, error) {
	var files localfs.Manifest
	leaseID := localfs.ID()
	err := s.with(ctx, func(current Selection) error {
		if err := sequence(current, expected); err != nil {
			return err
		}
		g, err := s.verifyGeneration(ctx, id)
		if err != nil {
			return err
		}
		files = g.Files
		body := current.Body
		body.Sequence++
		body.Leases = append(body.Leases, Lease{ID: leaseID, Generation: id, Kind: "restore"})
		_, err = s.commit(body)
		return err
	})
	if err != nil {
		return files, err
	}
	_, err = localfs.CopyFresh(ctx, filepath.Join(s.genPath(id), "payload"), destination)
	if err != nil {
		return files, err
	}
	if err := localfs.Verify(ctx, destination, files); err != nil {
		return files, err
	}
	return files, s.releaseLease(ctx, leaseID)
}

func (s Store) releaseLease(ctx context.Context, id string) error {
	return s.with(ctx, func(current Selection) error {
		body := current.Body
		before := len(body.Leases)
		body.Leases = slices.DeleteFunc(body.Leases, func(l Lease) bool { return l.ID == id })
		if len(body.Leases) == before {
			return document.Fail("LeaseMissing", "lease not present")
		}
		body.Sequence++
		_, err := s.commit(body)
		return err
	})
}

func (s Store) Pin(ctx context.Context, id string, pin bool, expected int64) (Selection, error) {
	var result Selection
	err := s.with(ctx, func(current Selection) error {
		if err := sequence(current, expected); err != nil {
			return err
		}
		if _, err := s.verifyGeneration(ctx, id); err != nil {
			return err
		}
		body := current.Body
		if pin && !slices.Contains(body.Pins, id) {
			if len(body.Pins) >= 64 {
				return document.Fail("ResourceLimit", "pin table full")
			}
			body.Pins = append(body.Pins, id)
		} else if !pin {
			body.Pins = slices.DeleteFunc(body.Pins, func(x string) bool { return x == id })
		}
		body.Sequence++
		var err error
		result, err = s.commit(body)
		return err
	})
	return result, err
}

func (s Store) Rollback(ctx context.Context, id string, expected int64) (Selection, error) {
	var result Selection
	err := s.with(ctx, func(current Selection) error {
		if err := sequence(current, expected); err != nil {
			return err
		}
		if current.Body.Work.State != "none" || len(current.Body.Leases) > 0 {
			return document.Fail("InstanceBusy", "resolve working copies and leases before rollback")
		}
		g, err := s.verifyGeneration(ctx, id)
		if err != nil {
			return err
		}
		if !g.ApplicationValidated {
			return document.Fail("ValidationRequired", "rollback target has no application validation")
		}
		body := current.Body
		body.Sequence++
		body.Previous = body.Active
		body.Active = id
		result, err = s.commit(body)
		return err
	})
	return result, err
}

// BeginRun persists ownership before execution. A crashed runner leaves its
// working copy/lease intact; no PID guess or lease age clears that authority.
func (s Store) BeginRun(ctx context.Context, expected int64) (Working, error) {
	var work Working
	err := s.with(ctx, func(current Selection) error {
		if err := sequence(current, expected); err != nil {
			return err
		}
		if current.Body.Work.State != "none" || len(current.Body.Leases) > 0 {
			return document.Fail("InstanceBusy", "uncommitted working copy or unresolved lease blocks a new writer")
		}
		id := localfs.ID()
		if _, err := localfs.CopyFresh(ctx, filepath.Join(s.genPath(current.Body.Active), "payload"), s.workingPath(id)); err != nil {
			return err
		}
		work = Working{ID: id, Generation: current.Body.Active, State: "running"}
		body := current.Body
		body.Sequence++
		body.Work = work
		body.Leases = append(body.Leases, Lease{ID: id, Generation: body.Active, Kind: "server"})
		_, err := s.commit(body)
		return err
	})
	return work, err
}

func (s Store) FinishRun(ctx context.Context, id string, clean, validated bool) error {
	return s.with(ctx, func(current Selection) error {
		if current.Body.Work.ID != id || current.Body.Work.State != "running" {
			return document.Fail("OwnershipLost", "working copy ownership changed")
		}
		body := current.Body
		body.Sequence++
		body.Work.Validated = clean && validated
		if clean {
			body.Work.State = "clean"
			body.Leases = slices.DeleteFunc(body.Leases, func(l Lease) bool { return l.ID == id })
		} else {
			body.Work.State = "faulted"
		}
		_, err := s.commit(body)
		return err
	})
}

func (s Store) WorkingPath(work Working) string { return s.workingPath(work.ID) }

// Reconcile is an explicit operator assertion, not an automatic PID check. It
// retains the abandoned working directory and clears only the named reference.
func (s Store) Reconcile(ctx context.Context, id string, confirmedStopped bool, expected int64) (Selection, error) {
	var result Selection
	if !confirmedStopped {
		return result, document.Fail("ConfirmationRequired", "confirm the original process/use is stopped before reconciling its lease")
	}
	err := s.with(ctx, func(current Selection) error {
		if err := sequence(current, expected); err != nil {
			return err
		}
		body := current.Body
		found := false
		if body.Work.ID == id {
			body.Work = Working{State: "none"}
			found = true
		}
		body.Leases = slices.DeleteFunc(body.Leases, func(l Lease) bool {
			if l.ID == id {
				found = true
				return true
			}
			return false
		})
		if !found {
			return document.Fail("LeaseMissing", "no such working/lease reference")
		}
		body.Sequence++
		var err error
		result, err = s.commit(body)
		return err
	})
	return result, err
}

func (s Store) GC(ctx context.Context, expected int64, apply bool) ([]string, error) {
	candidates := []string{}
	err := s.with(ctx, func(current Selection) error {
		if err := sequence(current, expected); err != nil {
			return err
		}
		active, err := s.verifyGeneration(ctx, current.Body.Active)
		if err != nil {
			return err
		}
		if !active.ApplicationValidated {
			return document.Fail("ValidationRequired", "collection requires an application-validated active checkpoint")
		}
		protected := append([]string{current.Body.Active, current.Body.Previous, current.Body.Work.Generation}, current.Body.Pins...)
		for _, l := range current.Body.Leases {
			protected = append(protected, l.Generation)
		}
		entries, err := os.ReadDir(filepath.Join(s.Root, "generations"))
		if err != nil {
			return err
		}
		for _, entry := range entries {
			id := entry.Name()
			if !entry.IsDir() || !validID(id) || slices.Contains(protected, id) {
				continue
			}
			if _, err := s.verifyGeneration(ctx, id); err != nil {
				continue
			}
			candidates = append(candidates, id)
			if apply {
				if err := s.point("before-delete"); err != nil {
					return err
				}
				if err := os.RemoveAll(s.genPath(id)); err != nil {
					return err
				}
				if err := localfs.SyncDir(filepath.Join(s.Root, "generations")); err != nil {
					return err
				}
				if err := s.point("after-delete"); err != nil {
					return err
				}
			}
		}
		return nil
	})
	sort.Strings(candidates)
	return candidates, err
}

func IsID(id string) bool { return validID(strings.TrimSpace(id)) && id == strings.TrimSpace(id) }
