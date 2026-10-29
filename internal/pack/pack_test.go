package pack

import (
	"context"
	"encoding/json"
	"modgrid.local/modgrid/internal/resolver"
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T) ([]byte, string) {
	t.Helper()
	lock, err := os.ReadFile("../../tests/fixtures/offline/expected.lock.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog resolver.Catalog
	b, err := os.ReadFile("../../tests/fixtures/offline/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &catalog); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(t.TempDir(), "cache")
	for _, a := range catalog.Artifacts {
		if err := Put(context.Background(), "../../tests/fixtures/offline/payloads/"+a.FileID+".txt", cache, a.Size, a.SHA512); err != nil {
			t.Fatal(err)
		}
	}
	return lock, cache
}

func TestExactExportAndCorruption(t *testing.T) {
	lock, cache := fixture(t)
	destination := filepath.Join(t.TempDir(), "pack")
	m, err := Export(context.Background(), lock, cache, "", destination)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyExport(context.Background(), destination); err != nil {
		t.Fatal(err)
	}
	if len(m.Files.Entries) != 6 {
		t.Fatalf("expected mods directory, four artifacts and lock, got %d", len(m.Files.Entries))
	}
	if _, err := Export(context.Background(), lock, cache, "", destination); err == nil {
		t.Fatal("overwrote existing export")
	}
	if err := os.WriteFile(filepath.Join(destination, "payload", "extra"), []byte("extra"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyExport(context.Background(), destination); err == nil {
		t.Fatal("accepted extra file")
	}
	l, _, err := resolver.ReadLock(lock, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, l.Artifacts[0].SHA512[7:]), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	failed := filepath.Join(t.TempDir(), "failed")
	if _, err := Export(context.Background(), lock, cache, "", failed); err == nil {
		t.Fatal("exported corrupt cache")
	}
	if _, err := os.Stat(failed); !os.IsNotExist(err) {
		t.Fatal("published failed stage")
	}
}

func TestMissingArtifactAndCancelledExport(t *testing.T) {
	lock, cache := fixture(t)
	l, _, err := resolver.ReadLock(lock, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(cache, l.Artifacts[0].SHA512[7:])); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "absent")
	if _, err := Export(context.Background(), lock, cache, "", dest); err == nil {
		t.Fatal("accepted missing bytes")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Export(ctx, lock, cache, "", dest); err == nil {
		t.Fatal("ignored cancellation")
	}
}
