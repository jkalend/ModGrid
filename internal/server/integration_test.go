package server

import (
	"archive/zip"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"io"
	"modgrid.local/modgrid/internal/document"
	"modgrid.local/modgrid/internal/instance"
	"modgrid.local/modgrid/internal/localfs"
	"modgrid.local/modgrid/internal/pack"
	"modgrid.local/modgrid/internal/resolver"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func fixtureJar(t *testing.T, root, java string) string {
	t.Helper()
	classes := filepath.Join(t.TempDir(), "classes")
	if err := os.Mkdir(classes, 0700); err != nil {
		t.Fatal(err)
	}
	javac := filepath.Join(filepath.Dir(java), "javac")
	if runtime.GOOS == "windows" {
		javac += ".exe"
	}
	loader := filepath.Join(root, "libraries", "net", "fabricmc", "fabric-loader", "0.19.5", "fabric-loader-0.19.5.jar")
	command := exec.Command(javac, "--release", "21", "-cp", loader, "-d", classes, "../../tests/fixture-mod/src/dev/modgrid/fixture/BootstrapFixture.java")
	if b, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fixture compile: %s %v", b, err)
	}
	jarPath := filepath.Join(t.TempDir(), "fixture.jar")
	f, err := os.Create(jarPath)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for _, name := range []string{"fabric.mod.json", "dev/modgrid/fixture/BootstrapFixture.class"} {
		var b []byte
		if name == "fabric.mod.json" {
			b, err = os.ReadFile("../../tests/fixture-mod/fabric.mod.json")
		} else {
			b, err = os.ReadFile(filepath.Join(classes, filepath.FromSlash(name)))
		}
		if err != nil {
			t.Fatal(err)
		}
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		header.SetMode(0644)
		w, err := z.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return jarPath
}

func realPayload(t *testing.T, template, java string) string {
	t.Helper()
	ctx := context.Background()
	jar := fixtureJar(t, template, java)
	jarBytes, err := os.ReadFile(jar)
	if err != nil {
		t.Fatal(err)
	}
	h := sha512.Sum512(jarBytes)
	var original resolver.Catalog
	cb, err := os.ReadFile("../../tests/fixtures/offline/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(cb, &original); err != nil {
		t.Fatal(err)
	}
	a := original.Artifacts[0]
	a.Project = "modgrid-fixture"
	a.FileID = "fixture-1"
	a.Version = "1.0.0"
	a.VersionID = "fixture-1.0.0"
	a.Source.Revision = "runtime-v1"
	a.Size = int64(len(jarBytes))
	a.SHA512 = "sha512:" + hex.EncodeToString(h[:])
	a.Sides = resolver.Sides{Client: "unsupported", Server: "required"}
	a.OriginalSides = "authored dedicated-server fixture"
	a.Dependencies = []resolver.Dependency{}
	a.Compatibility = "Explicit authored Fabric tuple; this test independently records actual boot behavior"
	c := resolver.Catalog{Schema: "modgrid.catalog.v1", ID: "runtime-authored-v1", Artifacts: []resolver.Artifact{a}}
	cb, err = json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	catalogID, err := resolver.CatalogIdentity(cb)
	if err != nil {
		t.Fatal(err)
	}
	p := resolver.PackSpec{Schema: "modgrid.pack.v1", ID: "runtime-fixture", CatalogID: c.ID, CatalogSHA256: catalogID, Target: resolver.Target{Minecraft: "1.21.1", Loader: resolver.Loader{ID: "fabric", Version: "0.19.5"}, JavaMajor: 21, Platform: runtime.GOOS + "/" + runtime.GOARCH, Side: "server"}, Roots: []resolver.Request{{Project: a.Project, Constraint: a.Version, Sides: []string{"server"}}}, Choices: []resolver.Choice{}, Configs: []resolver.Config{}}
	pb, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	_, lock, _, err := resolver.Resolve(pb, cb)
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	cache := filepath.Join(base, "cache")
	if err := pack.Put(ctx, jar, cache, a.Size, a.SHA512); err != nil {
		t.Fatal(err)
	}
	export := filepath.Join(base, "export")
	if _, err := pack.Export(ctx, lock, cache, "", export); err != nil {
		t.Fatal(err)
	}
	build := os.Getenv("MODGRID_TEST_JAVA_BUILD")
	if build == "" {
		t.Fatal("MODGRID_TEST_JAVA_BUILD is required")
	}
	dist := os.Getenv("MODGRID_TEST_JAVA_DISTRIBUTION_SHA256")
	if dist == "" {
		t.Fatal("MODGRID_TEST_JAVA_DISTRIBUTION_SHA256 is required")
	}
	rt, err := Capture(template, java, "fabric-server-launch.jar", build, dist)
	if err != nil {
		t.Fatal(err)
	}
	files := append([]RuntimeFile{}, rt.Files...)
	for _, name := range []string{"server.properties", "eula.txt"} {
		size, h, err := fileHash(filepath.Join(template, name))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, RuntimeFile{Path: name, Size: size, SHA512: h})
	}
	payload := filepath.Join(export, "payload")
	for _, f := range files {
		target := filepath.Join(payload, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		in, err := os.Open(filepath.Join(template, filepath.FromSlash(f.Path)))
		if err != nil {
			t.Fatal(err)
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			in.Close()
			t.Fatal(err)
		}
		_, copyErr := io.Copy(out, in)
		in.Close()
		syncErr := out.Sync()
		closeErr := out.Close()
		if copyErr != nil || syncErr != nil || closeErr != nil {
			t.Fatal(copyErr, syncErr, closeErr)
		}
	}
	if err := localfs.WriteJSON(filepath.Join(payload, "modgrid.runtime.json"), rt, false); err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestRealFabricCheckpointRestore(t *testing.T) {
	template, java := os.Getenv("MODGRID_TEST_RUNTIME"), os.Getenv("MODGRID_TEST_JAVA")
	if template == "" || java == "" {
		t.Skip("explicit real-runtime fixture and owner EULA setup are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	payload := realPayload(t, template, java)
	s := instance.Store{Root: filepath.Join(t.TempDir(), "instance")}
	sel, err := s.Initialize(ctx, payload)
	if err != nil {
		t.Fatal(err)
	}
	var logs testLog
	logs.t = t
	report, err := Run(ctx, s, java, sel.Body.Sequence, Options{Address: "127.0.0.1:25577", Marker: 246813579, InitializeMarker: true, CheckAndStop: true}, nil, &logs)
	if err != nil {
		t.Fatal(err)
	}
	if !report.StatusVerified || !report.MarkerVerified || !report.CleanStop {
		t.Fatalf("incomplete first boot: %#v", report)
	}
	sel, err = s.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := s.Checkpoint(ctx, sel.Body.Sequence)
	if err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(t.TempDir(), "scratch")
	if _, err := s.Restore(ctx, checkpoint.Body.Active, scratch, checkpoint.Body.Sequence); err != nil {
		t.Fatal(err)
	}
	// Restored bytes were independently enumerated and checked by Restore before
	// a second instance can boot and mutate its separate working copy.
	other := instance.Store{Root: filepath.Join(t.TempDir(), "restored-instance")}
	restored, err := other.Initialize(ctx, scratch)
	if err != nil {
		t.Fatal(err)
	}
	report, err = Run(ctx, other, java, restored.Body.Sequence, Options{Address: "127.0.0.1:25577", Marker: 246813579, CheckAndStop: true}, nil, &logs)
	if err != nil {
		t.Fatal(err)
	}
	if !report.StatusVerified || !report.MarkerVerified || !report.CleanStop {
		t.Fatalf("incomplete restored oracle: %#v", report)
	}
	b, err := document.Canonical(report)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Restored runtime evidence:", string(b))
}

type testLog struct{ t *testing.T }

func (l *testLog) Write(b []byte) (int, error) { l.t.Log(string(b)); return len(b), nil }
