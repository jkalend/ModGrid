package localfs

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"modgrid.local/modgrid/internal/document"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func Read(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, document.Fail("UnsupportedFile", "input must be regular")
	}
	return document.ReadDocument(f)
}

func Write(path string, b []byte, replace bool) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = Rename(f.Name(), path, replace); err != nil {
		return err
	}
	return SyncDir(dir)
}
func WriteJSON(path string, v any, replace bool) error {
	b, err := document.Canonical(v)
	if err != nil {
		return err
	}
	return Write(path, b, replace)
}

func Lock(ctx context.Context, path string) (func() error, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = regularHandle(f); err != nil {
		f.Close()
		return nil, err
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(15 * time.Millisecond)
	defer ticker.Stop()
	for {
		ok, err := tryLock(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		if ok {
			return func() error { err := unlock(f); closeErr := f.Close(); return errors.Join(err, closeErr) }, nil
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-deadline.C:
			f.Close()
			return nil, document.Fail("LockUnavailable", "lifecycle lock acquisition timed out")
		case <-ticker.C:
		}
	}
}

type Entry struct {
	Path   string `json:"path"`
	Type   string `json:"type"`
	Size   int64  `json:"size_bytes"`
	SHA256 string `json:"sha256"`
}
type Manifest struct {
	Schema  string  `json:"schema"`
	Entries []Entry `json:"entries"`
}

func Scan(ctx context.Context, path string) (Manifest, error) {
	m := Manifest{Schema: "modgrid.files.v1", Entries: []Entry{}}
	r, err := os.OpenRoot(path)
	if err != nil {
		return m, err
	}
	defer r.Close()
	keys := map[string]string{}
	var total int64
	err = fs.WalkDir(r.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		if len(m.Entries) >= MaxEntries {
			return document.Fail("ResourceLimit", "tree exceeds 10000 entries")
		}
		key, err := TreePath(name)
		if err != nil {
			return err
		}
		if prior, ok := keys[key]; ok && prior != name {
			return document.Fail("PathCollision", "portable path collision")
		}
		keys[key] = name
		if err := TargetPath(path, name); err != nil {
			return err
		}
		info, err := r.Lstat(name)
		if err != nil {
			return err
		}
		if info.IsDir() {
			m.Entries = append(m.Entries, Entry{Path: name, Type: "directory"})
			return nil
		}
		if !info.Mode().IsRegular() {
			return document.Fail("UnsupportedFile", "tree contains a link or special file")
		}
		f, err := r.Open(name)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := regularHandle(f); err != nil {
			return document.Fail("UnsupportedFile", err.Error())
		}
		before, err := f.Stat()
		if err != nil {
			return err
		}
		if !os.SameFile(info, before) || !before.Mode().IsRegular() {
			return document.Fail("SourceChanged", "file changed during scan")
		}
		if before.Size() > MaxFileBytes || total+before.Size() > MaxTreeBytes {
			return document.Fail("ResourceLimit", "file/tree byte budget exceeded")
		}
		h := sha256.New()
		n, err := io.Copy(h, io.LimitReader(&contextReader{ctx: ctx, r: f}, MaxFileBytes+1))
		if err != nil {
			return err
		}
		after, err := f.Stat()
		if err != nil {
			return err
		}
		if n != before.Size() || !before.ModTime().Equal(after.ModTime()) || before.Size() != after.Size() {
			return document.Fail("SourceChanged", "file changed during scan")
		}
		total += n
		if n > MaxFileBytes || total > MaxTreeBytes {
			return document.Fail("ResourceLimit", "stream byte budget exceeded")
		}
		m.Entries = append(m.Entries, Entry{Path: name, Type: "file", Size: n, SHA256: "sha256:" + hex.EncodeToString(h.Sum(nil))})
		return nil
	})
	sort.Slice(m.Entries, func(i, j int) bool { return m.Entries[i].Path < m.Entries[j].Path })
	return m, err
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(b) > 65536 {
		b = b[:65536]
	}
	return r.r.Read(b)
}

func Verify(ctx context.Context, path string, want Manifest) error {
	actual, err := Scan(ctx, path)
	if err != nil {
		return err
	}
	a, err := document.Canonical(actual)
	if err != nil {
		return err
	}
	b, err := document.Canonical(want)
	if err != nil {
		return err
	}
	if !bytes.Equal(a, b) {
		return document.Fail("TreeMismatch", "missing, extra or changed paths/bytes in tree")
	}
	return nil
}

// CopyFresh publishes a complete private stage to an absent destination. Every
// path is accessed through an open root; links and shared hardlinks are rejected.
func CopyFresh(ctx context.Context, source, destination string) (Manifest, error) {
	want, err := Scan(ctx, source)
	if err != nil {
		return want, err
	}
	parent := filepath.Dir(destination)
	stage, err := os.MkdirTemp(parent, ".copy-*")
	if err != nil {
		return want, err
	}
	defer os.RemoveAll(stage)
	if err := CopyInto(ctx, source, stage, want); err != nil {
		return want, err
	}
	if err := Rename(stage, destination, false); err != nil {
		return want, err
	}
	return want, SyncDir(parent)
}

// CopyInto requires an empty private destination owned by this operation.
func CopyInto(ctx context.Context, source, destination string, want Manifest) error {
	src, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer dst.Close()
	for _, e := range want.Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := TargetPath(destination, e.Path); err != nil {
			return err
		}
		if e.Type == "directory" {
			if err := dst.MkdirAll(e.Path, 0700); err != nil {
				return err
			}
			continue
		}
		if err := dst.MkdirAll(filepath.Dir(filepath.FromSlash(e.Path)), 0700); err != nil {
			return err
		}
		input, err := src.Open(e.Path)
		if err != nil {
			return err
		}
		if err := regularHandle(input); err != nil {
			input.Close()
			return err
		}
		output, err := dst.OpenFile(e.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			input.Close()
			return err
		}
		n, copyErr := io.Copy(output, io.LimitReader(&contextReader{ctx: ctx, r: input}, e.Size+1))
		input.Close()
		syncErr := output.Sync()
		closeErr := output.Close()
		if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
			return err
		}
		if n != e.Size {
			return document.Fail("SourceChanged", "copy length differs from manifest")
		}
	}
	if err := Verify(ctx, destination, want); err != nil {
		return err
	}
	if err := Verify(ctx, source, want); err != nil {
		return document.Fail("SourceChanged", err.Error())
	}
	return SyncTree(destination)
}

func SyncTree(root string) error {
	dirs := []string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := SyncDir(dirs[i]); err != nil {
			return err
		}
	}
	return nil
}
