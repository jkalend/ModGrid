package localfs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPathProfile(t *testing.T) {
	for _, p := range []string{"../x", "/x", "a//b", "C:/x", "a\\b", "a.", "CON.txt", "com1.a", "LPT¹", "a:b", "ä", "a b", strings.Repeat("a", 121)} {
		if _, err := Path(p); err == nil {
			t.Fatalf("accepted %q", p)
		}
	}
	key, err := Path("DIM-1/region/r.0.0.mca")
	if err != nil || key != "dim-1/region/r.0.0.mca" {
		t.Fatalf("%s %v", key, err)
	}
}

func TestCopyDetectsExtraCorruptAndLinks(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "world.dat"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	m, err := CopyFresh(context.Background(), source, destination)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(context.Background(), destination, m); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "world.dat"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Verify(context.Background(), destination, m); err == nil {
		t.Fatal("accepted corrupt data")
	}
	if err := os.Link(filepath.Join(source, "world.dat"), filepath.Join(source, "shared.dat")); err == nil {
		if _, err := Scan(context.Background(), source); err == nil {
			t.Fatal("accepted hardlink")
		}
		os.Remove(filepath.Join(source, "shared.dat"))
	}
	if err := os.Symlink(filepath.Join(source, "world.dat"), filepath.Join(source, "link.dat")); err == nil {
		if _, err := Scan(context.Background(), source); err == nil {
			t.Fatal("accepted symlink")
		}
	}
}

func TestLockHelper(t *testing.T) {
	if os.Getenv("MODGRID_TEST_LOCK_HELPER") != "1" {
		return
	}
	release, err := Lock(context.Background(), os.Getenv("MODGRID_TEST_LOCK_PATH"))
	if err != nil {
		os.Exit(2)
	}
	defer release()
	if err := os.WriteFile(os.Getenv("MODGRID_TEST_READY"), []byte("ready"), 0600); err != nil {
		os.Exit(3)
	}
	for {
		time.Sleep(time.Hour)
	}
}

func TestCrossProcessLockAndKilledOwner(t *testing.T) {
	root := t.TempDir()
	lock := filepath.Join(root, "lifecycle.lock")
	ready := filepath.Join(root, "ready")
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockHelper$")
	cmd.Env = append(os.Environ(), "MODGRID_TEST_LOCK_HELPER=1", "MODGRID_TEST_LOCK_PATH="+lock, "MODGRID_TEST_READY="+ready)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child lock owner did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if release, err := Lock(ctx, lock); err == nil {
		release()
		t.Fatal("second process acquired held lock")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	release, err := Lock(ctx2, lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
}
