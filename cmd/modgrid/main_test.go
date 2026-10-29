package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"modgrid.local/modgrid/internal/resolver"
)

const fixtures = "../../tests/fixtures/offline/"

func TestUnavailableCatalogReportsSource(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"catalog", "id", "--catalog", filepath.Join(t.TempDir(), "missing.json")}, &out, &out)
	var diagnostic *resolver.Error
	if !errors.As(err, &diagnostic) || diagnostic.Code != "SourceUnavailable" {
		t.Fatalf("unexpected diagnostic: %v", err)
	}
}

func TestCLIResolveVerifyAndFailedSolvePreservesLock(t *testing.T) {
	output := filepath.Join(t.TempDir(), "nested", "pack.lock")
	args := []string{"resolve", "--pack", fixtures + "pack.json", "--catalog", fixtures + "catalog.json", "--out", output, "--json"}
	var stdout, stderr bytes.Buffer
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"schema":"modgrid.report.v1"`) {
		t.Fatal(stdout.String())
	}
	before, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	// Replacing an existing lock works, and an unsatisfied solve cannot alter it.
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	id, err := os.ReadFile(fixtures + "expected.lock.id")
	if err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"lock", "verify", "--lock", output, "--pack", fixtures + "pack.json", "--catalog", fixtures + "catalog.json", "--expected-id", strings.TrimSpace(string(id))}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	pack, err := os.ReadFile(fixtures + "pack.json")
	if err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(t.TempDir(), "broken.json")
	pack = bytes.Replace(pack, []byte(`>=1.0.0,<3.0.0`), []byte(`>=9.0.0`), 1)
	if err := os.WriteFile(broken, pack, 0600); err != nil {
		t.Fatal(err)
	}
	args[2] = broken
	if err := run(args, &stdout, &stderr); err == nil {
		t.Fatal("unsatisfiable solve succeeded")
	}
	after, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed solve changed existing lock")
	}
	entries, err := os.ReadDir(filepath.Dir(output))
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files leaked: %v %v", entries, err)
	}
}

func TestPublicationFailureCleansTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "existing-directory")
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeLock(destination, []byte("lock")); err == nil {
		t.Fatal("replaced directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary file leak: %v %v", entries, err)
	}
}

func TestCLIRejectsInputOverwriteAndBadArguments(t *testing.T) {
	var out bytes.Buffer
	for _, args := range [][]string{
		{}, {"resolve"}, {"lock"}, {"catalog"}, {"unknown"},
		{"catalog", "id", "--catalog", fixtures + "catalog.json", "stray"},
		{"resolve", "--pack", fixtures + "pack.json", "--catalog", fixtures + "catalog.json", "--out", fixtures + "pack.json"},
	} {
		if err := run(args, &out, &out); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if err := run([]string{"--help"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"catalog", "id", "--catalog", fixtures + "catalog.json"}, &out, &out); err != nil {
		t.Fatal(err)
	}
}
