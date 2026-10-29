package resolver

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
)

var identifier = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
var sha256Pattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var sha512Pattern = regexp.MustCompile(`^sha512:[0-9a-f]{128}$`)
var portablePath = regexp.MustCompile(`^[a-z0-9._/-]{1,180}$`)

func validID(s string) bool { return identifier.MatchString(s) }
func textPresent(s string) bool {
	return strings.TrimSpace(s) != "" && !strings.ContainsFunc(s, unicode.IsControl)
}

func validPath(path string) bool {
	if !portablePath.MatchString(path) {
		return false
	}
	parts := strings.Split(path, "/")
	if len(parts) > 32 {
		return false
	}
	for _, p := range parts {
		if p == "" || p == "." || p == ".." || len(p) > 120 || strings.HasSuffix(p, ".") {
			return false
		}
		base := strings.Split(p, ".")[0]
		if slices.Contains([]string{"con", "prn", "aux", "nul"}, base) ||
			(len(base) == 4 && (strings.HasPrefix(base, "com") || strings.HasPrefix(base, "lpt")) && base[3] >= '1' && base[3] <= '9') {
			return false
		}
	}
	return true
}

func normalizeConstraint(s *string) error {
	terms, err := parseConstraint(*s)
	if err != nil {
		return err
	}
	parts := make([]string, len(terms))
	for i, t := range terms {
		parts[i] = fmt.Sprintf("%s%d.%d.%d", t.op, t.v[0], t.v[1], t.v[2])
	}
	sort.Strings(parts)
	*s = strings.Join(slices.Compact(parts), ",")
	return nil
}

func parseCatalog(b []byte) (Catalog, string, error) {
	var c Catalog
	if err := decode(b, &c); err != nil {
		return c, "", err
	}
	if c.Schema != "modgrid.catalog.v1" {
		return c, "", failure("UnsupportedSchema", "expected modgrid.catalog.v1")
	}
	if !validID(c.ID) {
		return c, "", failure("InvalidInput", "invalid catalog ID")
	}
	if len(c.Artifacts) > MaxArtifacts {
		return c, "", failure("ResourceLimit", "catalog exceeds 256 artifacts")
	}
	files, projects, versions := map[string]bool{}, map[string]bool{}, map[string]string{}
	edges := 0
	for i := range c.Artifacts {
		a := &c.Artifacts[i]
		if !validID(a.Project) || !validID(a.VersionID) || !validID(a.FileID) || !validID(a.Source.ID) || !validID(a.Source.Revision) {
			return c, "", failure("InvalidInput", "invalid artifact/source identifier")
		}
		if files[a.FileID] {
			return c, "", failure("DuplicateID", "duplicate file ID "+a.FileID)
		}
		files[a.FileID] = true
		projects[a.Project] = true
		if _, err := parseVersion(a.Version); err != nil {
			return c, "", err
		}
		key := a.Source.ID + "/" + a.Project + "/" + a.VersionID
		if previous, ok := versions[key]; ok && previous != a.Version {
			return c, "", failure("DuplicateID", "contradictory version identity "+key)
		}
		versions[key] = a.Version
		if !sha512Pattern.MatchString(a.SHA512) || a.Size < 0 || a.Size > 268435456 {
			return c, "", failure("InvalidInput", "invalid artifact SHA-512 or size (maximum 256 MiB)")
		}
		if _, err := parseVersion(a.Minecraft); err != nil {
			return c, "", err
		}
		if _, err := parseVersion(a.Loader.Version); err != nil {
			return c, "", err
		}
		if !validID(a.Loader.ID) || !textPresent(a.Compatibility) || !textPresent(a.Source.Facts) || !textPresent(a.OriginalSides) {
			return c, "", failure("InvalidInput", "artifact requires loader identity, original source/side facts and compatibility evidence")
		}
		for _, side := range []string{a.Sides.Client, a.Sides.Server} {
			if !slices.Contains([]string{"required", "optional", "unsupported"}, side) {
				return c, "", failure("UnknownSide", "unknown side for "+a.FileID)
			}
		}
		if a.Sides.Client == "unsupported" && a.Sides.Server == "unsupported" {
			return c, "", failure("UnknownSide", "artifact supports neither side: "+a.FileID)
		}
		if !slices.Contains([]string{"local-use", "unknown", "blocked"}, a.Rights.Status) || !textPresent(a.Rights.Evidence) {
			return c, "", failure("InvalidInput", "rights status and evidence are required for "+a.FileID)
		}
		if strings.ContainsFunc(a.License, unicode.IsControl) {
			return c, "", failure("InvalidInput", "license contains control characters")
		}
		deps := map[string]bool{}
		for j := range a.Dependencies {
			d := &a.Dependencies[j]
			if !validID(d.Project) || !textPresent(d.Original) {
				return c, "", failure("InvalidInput", "dependency requires project ID and original source fact")
			}
			if deps[d.Project] {
				return c, "", failure("DuplicateID", "duplicate dependency project in "+a.FileID)
			}
			deps[d.Project] = true
			if err := normalizeConstraint(&d.Constraint); err != nil {
				return c, "", err
			}
			edges++
		}
		sort.Slice(a.Dependencies, func(i, j int) bool { return a.Dependencies[i].Project < a.Dependencies[j].Project })
	}
	if len(projects) > MaxProjects || edges > MaxEdges {
		return c, "", failure("ResourceLimit", "catalog exceeds 64 projects or 1024 dependency edges")
	}
	sort.Slice(c.Artifacts, func(i, j int) bool { return c.Artifacts[i].FileID < c.Artifacts[j].FileID })
	canonicalBytes, err := canonical(c)
	if err != nil {
		return c, "", err
	}
	return c, digest(canonicalBytes), nil
}

// CatalogIdentity returns the normalized catalog identity to pin in a PackSpec.
func CatalogIdentity(b []byte) (string, error) {
	_, id, err := parseCatalog(b)
	return id, err
}

func parsePack(b []byte, catalog Catalog, catalogHash string) (PackSpec, error) {
	var p PackSpec
	if err := decode(b, &p); err != nil {
		return p, err
	}
	if p.Schema != "modgrid.pack.v1" {
		return p, failure("UnsupportedSchema", "expected modgrid.pack.v1")
	}
	if !validID(p.ID) || !validID(p.CatalogID) || !sha256Pattern.MatchString(p.CatalogSHA256) {
		return p, failure("InvalidInput", "invalid pack/catalog identity")
	}
	if p.CatalogID != catalog.ID || p.CatalogSHA256 != catalogHash {
		return p, failure("CatalogMismatch", "PackSpec catalog ID/digest does not match frozen catalog")
	}
	if p.Target.Minecraft != "1.21.1" || p.Target.Loader.ID != "fabric" || p.Target.JavaMajor != 21 ||
		!slices.Contains([]string{"windows/amd64", "linux/amd64"}, p.Target.Platform) || !slices.Contains([]string{"client", "server"}, p.Target.Side) {
		return p, failure("UnsupportedTarget", "fixture v1 supports Minecraft 1.21.1, Fabric, Java 21, client/server, windows/amd64 or linux/amd64")
	}
	if _, err := parseVersion(p.Target.Loader.Version); err != nil {
		return p, err
	}
	if len(p.Roots) == 0 || len(p.Roots) > MaxProjects || len(p.Choices) > MaxProjects || len(p.Configs) > MaxProjects {
		return p, failure("ResourceLimit", "pack requires 1..64 roots, <=64 choices and <=64 configs")
	}
	roots, choices, configs := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for i := range p.Roots {
		r := &p.Roots[i]
		if !validID(r.Project) || roots[r.Project] {
			return p, failure("DuplicateID", "invalid or duplicate root project "+r.Project)
		}
		roots[r.Project] = true
		if err := normalizeConstraint(&r.Constraint); err != nil {
			return p, err
		}
		sort.Strings(r.Sides)
		if len(r.Sides) == 0 || len(r.Sides) > 2 || len(slices.Compact(slices.Clone(r.Sides))) != len(r.Sides) {
			return p, failure("UnknownSide", "root requires unique explicit client/server sides")
		}
		for _, s := range r.Sides {
			if s != "client" && s != "server" {
				return p, failure("UnknownSide", "invalid root side")
			}
		}
	}
	for _, c := range p.Choices {
		if !validID(c.Project) || choices[c.Project] {
			return p, failure("DuplicateID", "invalid or duplicate optional choice "+c.Project)
		}
		choices[c.Project] = true
	}
	for _, c := range p.Configs {
		if !validPath(c.Path) || !sha256Pattern.MatchString(c.SHA256) || configs[c.Path] {
			return p, failure("InvalidInput", "invalid, unsafe or duplicate config path/digest")
		}
		configs[c.Path] = true
	}
	for path := range configs {
		parts := strings.Split(path, "/")
		for i := 1; i < len(parts); i++ {
			if configs[strings.Join(parts[:i], "/")] {
				return p, failure("InvalidInput", "config file/directory collision")
			}
		}
	}
	needChoice := func(project string) error {
		if !choices[project] {
			return failure("OptionalChoiceRequired", "record include=true/false for "+project)
		}
		return nil
	}
	for _, r := range p.Roots {
		if r.Optional {
			if err := needChoice(r.Project); err != nil {
				return p, err
			}
		}
	}
	for _, a := range catalog.Artifacts {
		side := a.Sides.Server
		if p.Target.Side == "client" {
			side = a.Sides.Client
		}
		if side == "optional" {
			if err := needChoice(a.Project); err != nil {
				return p, err
			}
		}
		for _, d := range a.Dependencies {
			if d.Optional {
				if err := needChoice(d.Project); err != nil {
					return p, err
				}
			}
		}
	}
	sort.Slice(p.Roots, func(i, j int) bool { return p.Roots[i].Project < p.Roots[j].Project })
	sort.Slice(p.Choices, func(i, j int) bool { return p.Choices[i].Project < p.Choices[j].Project })
	sort.Slice(p.Configs, func(i, j int) bool { return p.Configs[i].Path < p.Configs[j].Path })
	return p, nil
}
