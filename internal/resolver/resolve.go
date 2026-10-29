package resolver

import (
	"bytes"
	"fmt"
	"slices"
	"sort"
)

// Resolve reads only the supplied immutable bytes. It never accesses a provider,
// artifact cache, filesystem or network. Returned lock bytes have no trailing LF.
func Resolve(packBytes, catalogBytes []byte) (Lock, []byte, string, error) {
	return resolve(packBytes, catalogBytes, MaxDecisions)
}

func resolve(packBytes, catalogBytes []byte, budget int) (Lock, []byte, string, error) {
	var lock Lock
	c, catalogHash, err := parseCatalog(catalogBytes)
	if err != nil {
		return lock, nil, "", err
	}
	p, err := parsePack(packBytes, c, catalogHash)
	if err != nil {
		return lock, nil, "", err
	}
	choices := map[string]bool{}
	for _, choice := range p.Choices {
		choices[choice.Project] = choice.Include
	}
	index := map[string][]Artifact{}
	for _, a := range c.Artifacts {
		index[a.Project] = append(index[a.Project], a)
	}
	for project := range index {
		sort.Slice(index[project], func(i, j int) bool {
			a, b := index[project][i], index[project][j]
			av, _ := parseVersion(a.Version)
			bv, _ := parseVersion(b.Version)
			if cmp := compare(av, bv); cmp != 0 {
				return cmp > 0
			}
			return identity(a) < identity(b)
		})
	}
	roots := []Reason{}
	for _, r := range p.Roots {
		if slices.Contains(r.Sides, p.Target.Side) && (!r.Optional || choices[r.Project]) {
			roots = append(roots, Reason{From: "pack:" + p.ID, Project: r.Project, Constraint: r.Constraint})
		}
	}
	selected := map[string]Artifact{}
	remaining := budget
	// ponytail: bounded backtracking is exponential; replace the search with
	// conflict learning if real catalogs exhaust the 10,000-decision ceiling.
	var search func() ([]Reason, error)
	search = func() ([]Reason, error) {
		reasons := slices.Clone(roots)
		for _, project := range sortedKeys(selected) {
			a := selected[project]
			for _, d := range a.Dependencies {
				if !d.Optional || choices[d.Project] {
					reasons = append(reasons, Reason{From: identity(a), Project: d.Project, Constraint: d.Constraint})
				}
			}
		}
		sortReasons(reasons)
		requirements := map[string][]Reason{}
		for _, r := range reasons {
			requirements[r.Project] = append(requirements[r.Project], r)
		}
		unresolved := ""
		for _, project := range sortedKeys(requirements) {
			if a, ok := selected[project]; ok {
				v, _ := parseVersion(a.Version)
				for _, r := range requirements[project] {
					if !matches(v, r.Constraint) {
						return nil, conflict("Unsatisfiable", p.Target.Side+": selected "+identity(a)+" ("+a.Version+") violates "+r.Constraint, reasons)
					}
				}
			} else if unresolved == "" {
				unresolved = project
			}
		}
		if unresolved == "" {
			return reasons, nil
		}
		candidates, err := eligible(index[unresolved], requirements[unresolved], p.Target, choices)
		if err != nil {
			return nil, conflict(err.Code, err.Message, reasons)
		}
		var last error
		for _, a := range candidates {
			if remaining <= 0 {
				return nil, conflict("ResourceLimit", "candidate decision budget exhausted", reasons)
			}
			remaining--
			selected[unresolved] = a
			result, err := search()
			if err == nil {
				return result, nil
			}
			delete(selected, unresolved)
			if e, ok := err.(*Error); ok && e.Code == "ResourceLimit" {
				return nil, err
			}
			last = err
		}
		return nil, last
	}
	reasons, err := search()
	if err != nil {
		return lock, nil, "", err
	}
	lock = Lock{Schema: "modgrid.lock.v1", Pack: p, Artifacts: []Artifact{}, Reasons: reasons}
	for _, project := range sortedKeys(selected) {
		lock.Artifacts = append(lock.Artifacts, selected[project])
	}
	b, err := canonical(lock)
	if err != nil {
		return lock, nil, "", err
	}
	if len(b) > MaxDocumentBytes {
		return lock, nil, "", failure("ResourceLimit", "lock exceeds 4 MiB")
	}
	return lock, b, digest(b), nil
}

func eligible(artifacts []Artifact, reqs []Reason, target Target, choices map[string]bool) ([]Artifact, *Error) {
	project := reqs[0].Project
	if len(artifacts) == 0 {
		return nil, failure("MissingArtifact", target.Side+": no catalog artifact for "+project)
	}
	filtered := []Artifact{}
	for _, a := range artifacts {
		v, _ := parseVersion(a.Version)
		if slices.ContainsFunc(reqs, func(r Reason) bool { return !matches(v, r.Constraint) }) {
			continue
		}
		filtered = append(filtered, a)
	}
	if len(filtered) == 0 {
		return nil, failure("Unsatisfiable", target.Side+": no version satisfies all constraints for "+project)
	}
	filtered = slices.DeleteFunc(filtered, func(a Artifact) bool { return a.Minecraft != target.Minecraft || a.Loader != target.Loader })
	if len(filtered) == 0 {
		return nil, failure("IncompatibleTarget", target.Side+": no matching Minecraft/loader tuple for "+project)
	}
	filtered = slices.DeleteFunc(filtered, func(a Artifact) bool {
		side := a.Sides.Server
		if target.Side == "client" {
			side = a.Sides.Client
		}
		return side == "unsupported" || (side == "optional" && !choices[a.Project])
	})
	if len(filtered) == 0 {
		return nil, failure("SideConflict", target.Side+": required project is unsupported or explicitly excluded: "+project)
	}
	filtered = slices.DeleteFunc(filtered, func(a Artifact) bool { return a.Rights.Status != "local-use" })
	if len(filtered) == 0 {
		return nil, failure("RightsBlocked", target.Side+": local-use evidence unavailable/blocked for "+project)
	}
	return filtered, nil
}

func identity(a Artifact) string {
	return a.Source.ID + "/" + a.Project + "/" + a.VersionID + "/" + a.FileID
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortReasons(reasons []Reason) {
	sort.Slice(reasons, func(i, j int) bool {
		a, b := reasons[i], reasons[j]
		if a.Project != b.Project {
			return a.Project < b.Project
		}
		if a.From != b.From {
			return a.From < b.From
		}
		return a.Constraint < b.Constraint
	})
}

func conflict(code, message string, reasons []Reason) *Error {
	e := failure(code, message)
	for _, r := range reasons {
		e.Reasons = append(e.Reasons, fmt.Sprintf("%s requires %s %s", r.From, r.Project, r.Constraint))
	}
	return e
}

// Verify requires frozen inputs in S1. It validates schema, canonical bytes,
// every choice/reason, and an optional separately retained lock ID. S2 replay
// will consume the lock without invoking resolution or consulting a provider.
func Verify(lockBytes, packBytes, catalogBytes []byte, expectedID string) (Lock, string, error) {
	var lock Lock
	if err := decode(lockBytes, &lock); err != nil {
		return lock, "", err
	}
	if lock.Schema != "modgrid.lock.v1" {
		return lock, "", failure("UnsupportedSchema", "expected modgrid.lock.v1")
	}
	id := digest(lockBytes)
	if expectedID != "" {
		if !sha256Pattern.MatchString(expectedID) {
			return lock, "", failure("InvalidInput", "invalid expected SHA-256 lock ID")
		}
		if id != expectedID {
			return lock, "", failure("LockMismatch", "lock bytes differ from expected lock ID")
		}
	}
	_, expected, _, err := Resolve(packBytes, catalogBytes)
	if err != nil {
		return lock, "", err
	}
	if !bytes.Equal(expected, lockBytes) {
		return lock, "", failure("LockMismatch", "lock is noncanonical or differs from the frozen inputs/choices/reasons")
	}
	return lock, id, nil
}
