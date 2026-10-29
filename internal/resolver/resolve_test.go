package resolver

import (
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	jcs "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

const fixture = "../../tests/fixtures/offline"

func load(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixture, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func objects(t *testing.T) (PackSpec, Catalog) {
	t.Helper()
	var p PackSpec
	var c Catalog
	if err := decode(load(t, "pack.json"), &p); err != nil {
		t.Fatal(err)
	}
	if err := decode(load(t, "catalog.json"), &c); err != nil {
		t.Fatal(err)
	}
	return p, c
}

func encode(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func pinned(t *testing.T, p PackSpec, c Catalog) ([]byte, []byte) {
	t.Helper()
	cb := encode(t, c)
	id, err := CatalogIdentity(cb)
	if err != nil {
		t.Fatal(err)
	}
	p.CatalogSHA256 = id
	return encode(t, p), cb
}

func wantCode(t *testing.T, err error, code string) *Error {
	t.Helper()
	var diagnostic *Error
	if !errors.As(err, &diagnostic) || diagnostic.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
	return diagnostic
}

func TestAuthoredOracleAndGoldenLock(t *testing.T) {
	p, c := load(t, "pack.json"), load(t, "catalog.json")
	lock, b, id, err := Resolve(p, c)
	if err != nil {
		t.Fatal(err)
	}
	// Hand-written oracle: marker 2 is impossible, marker 1 and helper 1.5
	// form a satisfiable cycle, cosmetics is included, telemetry excluded.
	choices := map[string]string{}
	for _, a := range lock.Artifacts {
		choices[a.Project] = a.Version
	}
	want := map[string]string{"cosmetics": "1.0.0", "helper": "1.5.0", "marker": "1.0.0", "servertools": "1.0.0"}
	if !reflect.DeepEqual(choices, want) {
		t.Fatalf("choices: %v", choices)
	}
	wantReasons := []Reason{
		{From: "authored/marker/marker-1.0.0/marker-1", Project: "cosmetics", Constraint: "=1.0.0"},
		{From: "authored/marker/marker-1.0.0/marker-1", Project: "helper", Constraint: "<2.0.0,>=1.0.0"},
		{From: "authored/helper/helper-1.5.0/helper-15", Project: "marker", Constraint: "<2.0.0,>=1.0.0"},
		{From: "pack:offline-server", Project: "marker", Constraint: "<3.0.0,>=1.0.0"},
		{From: "pack:offline-server", Project: "servertools", Constraint: "=1.0.0"},
	}
	if !reflect.DeepEqual(lock.Reasons, wantReasons) {
		t.Fatalf("reasons: %#v", lock.Reasons)
	}
	if !bytes.Equal(b, load(t, "expected.lock.json")) {
		t.Fatal("canonical lock differs from golden")
	}
	if id != strings.TrimSpace(string(load(t, "expected.lock.id"))) {
		t.Fatalf("unexpected lock ID %s", id)
	}
	if _, gotID, err := Verify(b, p, c, id); err != nil || gotID != id {
		t.Fatalf("verify: %s %v", gotID, err)
	}
}

func TestMetadataOrderAndRepeatedSolves(t *testing.T) {
	_, expected, expectedID, err := Resolve(load(t, "pack.json"), load(t, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(42))
	for range 30 {
		p, c := objects(t)
		rng.Shuffle(len(c.Artifacts), func(i, j int) { c.Artifacts[i], c.Artifacts[j] = c.Artifacts[j], c.Artifacts[i] })
		for i := range c.Artifacts {
			d := c.Artifacts[i].Dependencies
			rng.Shuffle(len(d), func(i, j int) { d[i], d[j] = d[j], d[i] })
		}
		rng.Shuffle(len(p.Roots), func(i, j int) { p.Roots[i], p.Roots[j] = p.Roots[j], p.Roots[i] })
		for i := range p.Roots {
			slices.Reverse(p.Roots[i].Sides)
		}
		slices.Reverse(p.Choices)
		_, got, id, err := Resolve(encode(t, p), encode(t, c))
		if err != nil || !bytes.Equal(got, expected) || id != expectedID {
			t.Fatalf("order changed result: %s %v", id, err)
		}
	}
}

func TestSourceTieBreak(t *testing.T) {
	p, c := objects(t)
	for _, a := range c.Artifacts {
		if a.FileID == "helper-15" {
			a.FileID = "alternative-helper"
			a.Source.ID = "zeta"
			c.Artifacts = append(c.Artifacts, a)
			break
		}
	}
	pb, cb := pinned(t, p, c)
	lock, _, _, err := Resolve(pb, cb)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range lock.Artifacts {
		if a.Project == "helper" && a.FileID != "helper-15" {
			t.Fatal("unstable source tie break")
		}
	}
}

func TestAgainstSmallExhaustiveOracle(t *testing.T) {
	// Independently enumerate every assignment for three two-version projects.
	// Random directed exact dependencies include diamonds and conflicting cycles.
	rng := rand.New(rand.NewSource(73))
	for trial := range 60 {
		p, c := objects(t)
		base := c.Artifacts[0]
		c.Artifacts = []Artifact{}
		p.Roots = []Request{}
		p.Choices = []Choice{}
		for _, project := range []string{"a", "b", "c"} {
			p.Roots = append(p.Roots, Request{Project: project, Constraint: ">=1.0.0,<3.0.0", Sides: []string{"server"}})
			for v := 1; v <= 2; v++ {
				a := base
				a.Project, a.Version = project, fmt.Sprintf("%d.0.0", v)
				a.FileID, a.VersionID = fmt.Sprintf("%s-%d", project, v), fmt.Sprintf("%s-v%d", project, v)
				a.Dependencies = []Dependency{}
				for _, target := range []string{"a", "b", "c"} {
					if rng.Intn(3) == 0 {
						constraint := fmt.Sprintf("%d.0.0", 1+rng.Intn(2))
						a.Dependencies = append(a.Dependencies, Dependency{Project: target, Constraint: constraint, Original: "authored exact dependency " + constraint})
					}
				}
				c.Artifacts = append(c.Artifacts, a)
			}
		}
		var expected map[string]string
	outer:
		for av := 2; av >= 1; av-- {
			for bv := 2; bv >= 1; bv-- {
				for cv := 2; cv >= 1; cv-- {
					assignment := map[string]string{"a": fmt.Sprintf("%d.0.0", av), "b": fmt.Sprintf("%d.0.0", bv), "c": fmt.Sprintf("%d.0.0", cv)}
					valid := true
					for _, a := range c.Artifacts {
						if assignment[a.Project] != a.Version {
							continue
						}
						for _, d := range a.Dependencies {
							if assignment[d.Project] != d.Constraint {
								valid = false
							}
						}
					}
					if valid {
						expected = assignment
						break outer
					}
				}
			}
		}
		pb, cb := pinned(t, p, c)
		lock, _, _, err := Resolve(pb, cb)
		if expected == nil {
			wantCode(t, err, "Unsatisfiable")
			continue
		}
		if err != nil {
			t.Fatalf("trial %d: %v", trial, err)
		}
		actual := map[string]string{}
		for _, a := range lock.Artifacts {
			actual[a.Project] = a.Version
		}
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("trial %d: got %v, want %v", trial, actual, expected)
		}
	}
}

func TestClientAndOptionalChoices(t *testing.T) {
	p, c := objects(t)
	p.Target.Side = "client"
	p.Choices[1].Include = true
	pb, cb := pinned(t, p, c)
	lock, _, _, err := Resolve(pb, cb)
	if err != nil {
		t.Fatal(err)
	}
	projects := []string{}
	for _, a := range lock.Artifacts {
		projects = append(projects, a.Project)
	}
	if !reflect.DeepEqual(projects, []string{"clienthud", "cosmetics", "helper", "marker", "telemetry"}) {
		t.Fatal(projects)
	}
	// Excluding an optional dependency removes its branch entirely.
	p.Choices[0].Include = false
	pb, cb = pinned(t, p, c)
	lock, _, _, err = Resolve(pb, cb)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range lock.Artifacts {
		if a.Project == "cosmetics" {
			t.Fatal("excluded optional dependency selected")
		}
	}
}

func TestFailuresAtResolutionBoundary(t *testing.T) {
	tests := []struct {
		name, code string
		mutate     func(*PackSpec, *Catalog)
	}{
		{"conflicting cycle", "Unsatisfiable", func(p *PackSpec, c *Catalog) {
			c.Artifacts[1].Dependencies[0].Constraint = ">=2.0.0"
			c.Artifacts = c.Artifacts[1:]
		}},
		{"missing dependency", "MissingArtifact", func(p *PackSpec, c *Catalog) { c.Artifacts[2].Dependencies[0].Project = "absent" }},
		{"incompatible loader", "IncompatibleTarget", func(p *PackSpec, c *Catalog) {
			for i := range c.Artifacts {
				if c.Artifacts[i].Project == "marker" {
					c.Artifacts[i].Loader.ID = "forge"
				}
			}
		}},
		{"incompatible game", "IncompatibleTarget", func(p *PackSpec, c *Catalog) {
			for i := range c.Artifacts {
				if c.Artifacts[i].Project == "marker" {
					c.Artifacts[i].Minecraft = "1.20.1"
				}
			}
		}},
		{"side mismatch", "SideConflict", func(p *PackSpec, c *Catalog) { p.Roots[1].Sides = []string{"server"} }},
		{"blocked rights", "RightsBlocked", func(p *PackSpec, c *Catalog) { c.Artifacts[5].Rights.Status = "blocked" }},
		{"unknown rights", "RightsBlocked", func(p *PackSpec, c *Catalog) { c.Artifacts[5].Rights.Status = "unknown" }},
		{"optional side excluded but required", "SideConflict", func(p *PackSpec, c *Catalog) {
			p.Roots = append(p.Roots, Request{Project: "cosmetics", Constraint: "1.0.0", Sides: []string{"server"}})
			p.Choices[0].Include = false
		}},
		{"unsupported target", "UnsupportedTarget", func(p *PackSpec, c *Catalog) { p.Target.JavaMajor = 17 }},
		{"missing choice", "OptionalChoiceRequired", func(p *PackSpec, c *Catalog) { p.Choices = p.Choices[:1] }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, c := objects(t)
			tt.mutate(&p, &c)
			pb, cb := pinned(t, p, c)
			_, _, _, err := Resolve(pb, cb)
			diagnostic := wantCode(t, err, tt.code)
			if slices.Contains([]string{"Unsatisfiable", "MissingArtifact", "IncompatibleTarget", "SideConflict", "RightsBlocked"}, tt.code) && len(diagnostic.Reasons) == 0 {
				t.Fatal("missing actionable reason chain")
			}
			_, _, _, again := Resolve(pb, cb)
			if fmt.Sprint(err) != fmt.Sprint(again) {
				t.Fatal("unstable error")
			}
		})
	}
}

func TestInvalidCatalog(t *testing.T) {
	tests := []struct {
		name, code string
		mutate     func(*Catalog)
	}{
		{"duplicate file", "DuplicateID", func(c *Catalog) { c.Artifacts = append(c.Artifacts, c.Artifacts[0]) }},
		{"duplicate dependency", "DuplicateID", func(c *Catalog) { a := &c.Artifacts[2]; a.Dependencies = append(a.Dependencies, a.Dependencies[0]) }},
		{"contradictory version ID", "DuplicateID", func(c *Catalog) {
			a := c.Artifacts[0]
			a.FileID = "other"
			a.Version = "9.0.0"
			c.Artifacts = append(c.Artifacts, a)
		}},
		{"unknown side", "UnknownSide", func(c *Catalog) { c.Artifacts[0].Sides.Client = "unknown" }},
		{"bad digest", "InvalidInput", func(c *Catalog) { c.Artifacts[0].SHA512 = "sha256:" + strings.Repeat("a", 64) }},
		{"unsupported range", "UnsupportedConstraint", func(c *Catalog) { c.Artifacts[2].Dependencies[0].Constraint = "^1.0.0" }},
		{"unsupported schema", "UnsupportedSchema", func(c *Catalog) { c.Schema = "modgrid.catalog.v2" }},
		{"artifact size", "InvalidInput", func(c *Catalog) { c.Artifacts[0].Size = 268435457 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, c := objects(t)
			tt.mutate(&c)
			_, err := CatalogIdentity(encode(t, c))
			wantCode(t, err, tt.code)
		})
	}
}

func TestCatalogPinAndLockTampering(t *testing.T) {
	p, c := objects(t)
	c.Artifacts[0].Source.Revision = "changed"
	_, _, _, err := Resolve(encode(t, p), encode(t, c))
	wantCode(t, err, "CatalogMismatch")
	pb, cb := load(t, "pack.json"), load(t, "catalog.json")
	lock, b, id, err := Resolve(pb, cb)
	if err != nil {
		t.Fatal(err)
	}
	lock.Artifacts[0].Size++
	tampered, err := canonical(lock)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Verify(tampered, pb, cb, "")
	wantCode(t, err, "LockMismatch")
	_, _, err = Verify(append(slices.Clone(b), '\n'), pb, cb, "")
	wantCode(t, err, "LockMismatch")
	_, _, err = Verify(b, pb, cb, "sha256:"+strings.Repeat("0", 64))
	wantCode(t, err, "LockMismatch")
	_, _, err = Verify(b, pb, cb, "invalid")
	wantCode(t, err, "InvalidInput")
	if _, _, err := Verify(b, pb, cb, id); err != nil {
		t.Fatal(err)
	}
}

func TestBudgets(t *testing.T) {
	_, _, _, err := resolve(load(t, "pack.json"), load(t, "catalog.json"), 1)
	wantCode(t, err, "ResourceLimit")
	_, err = ReadDocument(strings.NewReader(strings.Repeat(" ", MaxDocumentBytes+1)))
	wantCode(t, err, "ResourceLimit")
	_, c := objects(t)
	for len(c.Artifacts) <= MaxArtifacts {
		a := c.Artifacts[0]
		a.FileID = fmt.Sprintf("extra-%d", len(c.Artifacts))
		c.Artifacts = append(c.Artifacts, a)
	}
	_, err = CatalogIdentity(encode(t, c))
	wantCode(t, err, "ResourceLimit")
	_, err = parseConstraint(strings.Repeat(">1.0.0,", 8) + ">1.0.0")
	wantCode(t, err, "ResourceLimit")
	_, c = objects(t)
	base := c.Artifacts[0]
	c.Artifacts = []Artifact{}
	for i := range MaxProjects + 1 {
		a := base
		a.Project = fmt.Sprintf("project-%d", i)
		a.FileID = a.Project
		c.Artifacts = append(c.Artifacts, a)
	}
	_, err = CatalogIdentity(encode(t, c))
	wantCode(t, err, "ResourceLimit")
	c.Artifacts = c.Artifacts[:18]
	for i := range c.Artifacts {
		c.Artifacts[i].Dependencies = []Dependency{}
		for j := range 60 {
			c.Artifacts[i].Dependencies = append(c.Artifacts[i].Dependencies, Dependency{Project: fmt.Sprintf("dep-%d", j), Constraint: "1.0.0", Original: "authored budget test"})
		}
	}
	_, err = CatalogIdentity(encode(t, c))
	wantCode(t, err, "ResourceLimit")
}

func TestStrictJSON(t *testing.T) {
	valid := string(load(t, "pack.json"))
	tests := []struct{ name, input string }{
		{"duplicate", strings.Replace(valid, `"schema":`, `"schema":"modgrid.pack.v1","schema":`, 1)},
		{"case alias", strings.Replace(valid, `"schema":`, `"Schema":`, 1)},
		{"unknown", strings.Replace(valid, `"schema":`, `"extra":true,"schema":`, 1)},
		{"missing", strings.Replace(valid, `"configs": []`, `"absent": []`, 1)},
		{"null", strings.Replace(valid, `"configs": []`, `"configs": null`, 1)},
		{"float", strings.Replace(valid, `"java_major": 21`, `"java_major": 21.0`, 1)},
		{"negative", strings.Replace(valid, `"java_major": 21`, `"java_major": -1`, 1)},
		{"unsafe integer", strings.Replace(valid, `"java_major": 21`, `"java_major": 9007199254740992`, 1)},
		{"trailing JSON", valid + `{}`},
		{"unpaired high", strings.Replace(valid, `offline-server`, `\ud800`, 1)},
		{"unpaired low", strings.Replace(valid, `offline-server`, `\udfff`, 1)},
		{"bad pair", strings.Replace(valid, `offline-server`, `\ud800\u0000`, 1)},
		{"reversed pair", strings.Replace(valid, `offline-server`, `\udfff\ud800`, 1)},
		{"invalid UTF8", strings.Replace(valid, `offline-server`, string([]byte{0xff}), 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { var p PackSpec; wantCode(t, decode([]byte(tt.input), &p), "InvalidInput") })
	}
	var p PackSpec
	wantCode(t, decode([]byte(strings.Repeat("[", 34)+strings.Repeat("]", 34)), &p), "ResourceLimit")
	wantCode(t, decode([]byte(strings.Replace(valid, `offline-server`, strings.Repeat("a", 2049), 1)), &p), "ResourceLimit")
	// Valid pairs and escaped literal backslashes are not rejected by the guard.
	for _, s := range []string{`"\ud83d\ude00"`, `"\\ud800"`, `"�"`} {
		if err := checkSurrogates([]byte(s)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVersionGrammar(t *testing.T) {
	for _, s := range []string{"^1.0.0", "1.*", "1.0.0 || 2.0.0", "1.0.0-beta", "1.0", "01.0.0", ">=1.0.0 <2.0.0", "\u00a01.0.0", ""} {
		if _, err := parseConstraint(s); err == nil {
			t.Fatalf("accepted %q", s)
		}
	}
	for _, s := range []string{"1.0.0", "=1.0.0", ">=1.0.0, <2.0.0", ">0.9.9,<=1.0.0"} {
		if _, err := parseConstraint(s); err != nil {
			t.Fatal(err)
		}
	}
	v, _ := parseVersion("1.10.0")
	if !matches(v, ">1.9.9,<=1.10.0") || matches(v, "<1.10.0") {
		t.Fatal("numeric comparison")
	}
	if _, err := parseVersion("4294967296.0.0"); err == nil {
		t.Fatal("component overflow")
	}
}

func TestConfigPaths(t *testing.T) {
	for _, s := range []string{"../secret", "/root", "c:/file", `config\file`, "con.txt", "lpt1/config", "a./b", "a//b", "a/../b", "Upper.txt"} {
		if validPath(s) {
			t.Fatalf("accepted %q", s)
		}
	}
	p, c := objects(t)
	p.Configs = []Config{{Path: "config", SHA256: "sha256:" + strings.Repeat("a", 64)}, {Path: "config/a.json", SHA256: "sha256:" + strings.Repeat("b", 64)}}
	pb, cb := pinned(t, p, c)
	_, _, _, err := Resolve(pb, cb)
	wantCode(t, err, "InvalidInput")
}

func TestJCSVectors(t *testing.T) {
	// RFC 8785 section 3.2.2 example, independently specified expected bytes.
	in := []byte(`{"numbers":[333333333.33333329,1E30,4.50,2e-3,0.000000000000000000000000001],"literals":[null,true,false]}`)
	want := `{"literals":[null,true,false],"numbers":[333333333.3333333,1e+30,4.5,0.002,1e-27]}`
	got, err := jcs.Transform(in)
	if err != nil || string(got) != want {
		t.Fatalf("JCS numeric vector: %s %v", got, err)
	}
	got, err = jcs.Transform([]byte(`{"דּ":7,"😀":6,"€":5,"ö":4,"\u0080":3,"1":2,"\r":1}`))
	want = `{"\r":1,"1":2,"` + "\u0080" + `":3,"ö":4,"€":5,"😀":6,"דּ":7}`
	if err != nil || string(got) != want {
		t.Fatalf("JCS UTF-16 ordering: %s %v", got, err)
	}
}

func TestPayloadMetadata(t *testing.T) {
	_, c := objects(t)
	for _, a := range c.Artifacts {
		b := load(t, "payloads/"+a.FileID+".txt")
		hash := sha512.Sum512(b)
		if a.Size != int64(len(b)) || a.SHA512 != "sha512:"+hex.EncodeToString(hash[:]) {
			t.Fatal("fixture payload drift: " + a.FileID)
		}
	}
}

func FuzzResolve(f *testing.F) {
	p, err := os.ReadFile(filepath.Join(fixture, "pack.json"))
	if err != nil {
		f.Fatal(err)
	}
	c, err := os.ReadFile(filepath.Join(fixture, "catalog.json"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(p, c)
	f.Add([]byte(`{}`), []byte(`{}`))
	f.Add([]byte(`{"id":"\ud800\u0000"}`), c)
	f.Fuzz(func(t *testing.T, p, c []byte) { _, _, _, _ = resolve(p, c, 100) })
}

func FuzzStrictJSON(f *testing.F) {
	f.Add([]byte(`{"name":"\ud83d\ude00"}`))
	f.Add([]byte(`{"name":"\ud800\u0000"}`))
	f.Add([]byte(`{"name":"a","name":"b"}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 16384 {
			t.Skip()
		}
		var value struct {
			Name string `json:"name"`
		}
		_ = decode(b, &value)
	})
}
