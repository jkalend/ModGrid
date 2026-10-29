package instance

import (
	"context"
	"encoding/json"
	"errors"
	"modgrid.local/modgrid/internal/pack"
	"modgrid.local/modgrid/internal/resolver"
	"os"
	"path/filepath"
	"testing"
)

func setup(t *testing.T) (Store, Selection) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	var catalog resolver.Catalog
	b, err := os.ReadFile("../../tests/fixtures/offline/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &catalog); err != nil {
		t.Fatal(err)
	}
	for _, a := range catalog.Artifacts {
		if err := pack.Put(ctx, "../../tests/fixtures/offline/payloads/"+a.FileID+".txt", cache, a.Size, a.SHA512); err != nil {
			t.Fatal(err)
		}
	}
	lock, err := os.ReadFile("../../tests/fixtures/offline/expected.lock.json")
	if err != nil {
		t.Fatal(err)
	}
	export := filepath.Join(root, "export")
	if _, err := pack.Export(ctx, lock, cache, "", export); err != nil {
		t.Fatal(err)
	}
	s := Store{Root: filepath.Join(root, "instance")}
	sel, err := s.Initialize(ctx, filepath.Join(export, "payload"))
	if err != nil {
		t.Fatal(err)
	}
	return s, sel
}

// This fixture simulates only the lifecycle result for filesystem transaction
// tests. It is not evidence of a Minecraft start, world oracle or compatibility.
func cleanFixtureRun(t *testing.T, s Store, sel Selection, world string) Selection {
	t.Helper()
	ctx := context.Background()
	work, err := s.BeginRun(ctx, sel.Body.Sequence)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(s.WorkingPath(work), "world"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.WorkingPath(work), "world", "fixture.dat"), []byte(world), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRun(ctx, work.ID, true, true); err != nil {
		t.Fatal(err)
	}
	current, err := s.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return current
}

func TestCheckpointRestoreRollbackPairsWorldAndPack(t *testing.T) {
	s, sel := setup(t)
	ctx := context.Background()
	sel = cleanFixtureRun(t, s, sel, "first-world")
	first, err := s.Checkpoint(ctx, sel.Body.Sequence)
	if err != nil {
		t.Fatal(err)
	}
	if first.Body.Previous != sel.Body.Active || first.Body.Active == sel.Body.Active {
		t.Fatal("invalid checkpoint authority")
	}
	sel = cleanFixtureRun(t, s, first, "second-world")
	second, err := s.Checkpoint(ctx, sel.Body.Sequence)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "restored")
	if _, err := s.Restore(ctx, first.Body.Active, dest, second.Body.Sequence); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "world", "fixture.dat"))
	if err != nil || string(b) != "first-world" {
		t.Fatalf("restore world: %s %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "modgrid.lock.json")); err != nil {
		t.Fatal(err)
	}
	current, err := s.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rolled, err := s.Rollback(ctx, first.Body.Active, current.Body.Sequence)
	if err != nil {
		t.Fatal(err)
	}
	if rolled.Body.Active != first.Body.Active || rolled.Body.Previous != second.Body.Active {
		t.Fatal("rollback identity mismatch")
	}
}

func TestFaultsPreserveValidOldOrNewSelector(t *testing.T) {
	for _, point := range []string{"after-data", "after-manifest", "after-marker", "after-install", "before-selector", "after-selector"} {
		t.Run(point, func(t *testing.T) {
			s, sel := setup(t)
			sel = cleanFixtureRun(t, s, sel, "marker")
			s.Fault = func(p string) error {
				if p == point {
					return errors.New("injected I/O failure")
				}
				return nil
			}
			if _, err := s.Checkpoint(context.Background(), sel.Body.Sequence); err == nil {
				t.Fatal("fault not reached")
			}
			s.Fault = nil
			after, err := s.Inspect(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if point == "after-selector" {
				if after.Body.Active == sel.Body.Active {
					t.Fatal("commit did not become observable")
				}
			} else if after.Body.Active != sel.Body.Active {
				t.Fatal("failed publication selected candidate")
			}
		})
	}
}

func TestFaultedRunAndStaleSequenceBlockMutation(t *testing.T) {
	s, sel := setup(t)
	ctx := context.Background()
	work, err := s.BeginRun(ctx, sel.Body.Sequence)
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginRun(ctx, current.Body.Sequence); err == nil {
		t.Fatal("duplicate writer")
	}
	if _, err := s.Checkpoint(ctx, current.Body.Sequence); err == nil {
		t.Fatal("checkpointed running server")
	}
	if err := s.FinishRun(ctx, work.ID, false, false); err != nil {
		t.Fatal(err)
	}
	current, err = s.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if current.Body.Work.State != "faulted" || len(current.Body.Leases) != 1 {
		t.Fatal("lost faulted run/lease")
	}
	if _, err := s.Reconcile(ctx, work.ID, false, current.Body.Sequence); err == nil {
		t.Fatal("silently cleared unknown process")
	}
	if _, err := s.Reconcile(ctx, work.ID, true, sel.Body.Sequence); err == nil {
		t.Fatal("accepted stale mutation")
	}
	if _, err := s.Reconcile(ctx, work.ID, true, current.Body.Sequence); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.WorkingPath(work)); err != nil {
		t.Fatal("removed recovery material")
	}
}

func TestGCProtectsReferencesAndCorruptionBlocksAll(t *testing.T) {
	s, sel := setup(t)
	ctx := context.Background()
	historical := sel.Body.Active
	for _, w := range []string{"one", "two", "three"} {
		sel = cleanFixtureRun(t, s, sel, w)
		var err error
		sel, err = s.Checkpoint(ctx, sel.Body.Sequence)
		if err != nil {
			t.Fatal(err)
		}
	}
	pinned, err := s.Pin(ctx, historical, true, sel.Body.Sequence)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := s.GC(ctx, pinned.Body.Sequence, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range candidates {
		if id == historical || id == pinned.Body.Active || id == pinned.Body.Previous {
			t.Fatal("collected protected generation")
		}
	}
	if _, err := os.Stat(s.genPath(historical)); err != nil {
		t.Fatal("pin lost")
	}
	if err := os.WriteFile(filepath.Join(s.Root, "selection.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GC(ctx, pinned.Body.Sequence, true); err == nil {
		t.Fatal("collected with corrupt selector")
	}
	if _, err := s.Inspect(ctx); err == nil {
		t.Fatal("auto-recovered invalid selector")
	}
}
