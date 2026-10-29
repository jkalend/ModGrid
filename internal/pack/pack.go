// Package pack replays only locked bytes into a fresh, verified export.
package pack

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"modgrid.local/modgrid/internal/document"
	"modgrid.local/modgrid/internal/localfs"
	"modgrid.local/modgrid/internal/resolver"
	"os"
	"path/filepath"
	"strings"
)

type ExportManifest struct {
	Schema string           `json:"schema"`
	LockID string           `json:"lock_id"`
	Files  localfs.Manifest `json:"files"`
}

func hashFor(expected string) (hash.Hash, error) {
	if strings.HasPrefix(expected, "sha512:") && len(expected) == 135 {
		if _, err := hex.DecodeString(expected[7:]); err == nil {
			return sha512.New(), nil
		}
	}
	if strings.HasPrefix(expected, "sha256:") && len(expected) == 71 {
		if _, err := hex.DecodeString(expected[7:]); err == nil {
			return sha256.New(), nil
		}
	}
	return nil, document.Fail("InvalidDigest", "expected tagged SHA-256 or SHA-512")
}

func copyVerified(ctx context.Context, input io.Reader, output io.Writer, size int64, expected string) error {
	if size < 0 || size > localfs.MaxFileBytes {
		return document.Fail("ResourceLimit", "artifact exceeds 256 MiB")
	}
	h, err := hashFor(expected)
	if err != nil {
		return err
	}
	buf := make([]byte, 65536)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := input.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > size {
				return document.Fail("CorruptArtifact", "artifact exceeds locked size")
			}
			if _, err := io.MultiWriter(output, h).Write(buf[:n]); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
		if n == 0 {
			return io.ErrNoProgress
		}
	}
	if total != size || hex.EncodeToString(h.Sum(nil)) != strings.SplitN(expected, ":", 2)[1] {
		return document.Fail("CorruptArtifact", "artifact size/hash differs from locked bytes")
	}
	return nil
}

// Put adds a verified artifact to the local cache by SHA-512, without a source
// metadata lookup. A cache hit is verified again instead of trusting its name.
func Put(ctx context.Context, file, cache string, size int64, sha512 string) error {
	if !strings.HasPrefix(sha512, "sha512:") {
		return document.Fail("InvalidDigest", "cache requires SHA-512")
	}
	if _, err := hashFor(sha512); err != nil {
		return err
	}
	if err := os.MkdirAll(cache, 0700); err != nil {
		return err
	}
	final := filepath.Join(cache, sha512[7:])
	if _, err := os.Lstat(final); err == nil {
		return VerifyFile(ctx, final, size, sha512)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	in, err := os.Open(file)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return document.Fail("UnsupportedFile", "artifact source must be regular")
	}
	tmp, err := os.CreateTemp(cache, ".artifact-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	copyErr := copyVerified(ctx, in, tmp, size, sha512)
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
		return err
	}
	if err := localfs.Rename(tmp.Name(), final, false); err != nil {
		if _, statErr := os.Lstat(final); statErr == nil {
			return VerifyFile(ctx, final, size, sha512)
		}
		return err
	}
	return localfs.SyncDir(cache)
}

func VerifyFile(ctx context.Context, file string, size int64, digest string) error {
	info, err := os.Lstat(file)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return document.Fail("UnsupportedFile", "artifact cannot be a link or special file")
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	return copyVerified(ctx, f, io.Discard, size, digest)
}

func Export(ctx context.Context, lockBytes []byte, cache, configs, destination string) (ExportManifest, error) {
	var report ExportManifest
	lock, id, err := resolver.ReadLock(lockBytes, "")
	if err != nil {
		return report, err
	}
	if _, err := os.Lstat(destination); err == nil {
		return report, document.Fail("DestinationExists", "export requires an absent destination")
	} else if !errors.Is(err, os.ErrNotExist) {
		return report, err
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return report, err
	}
	stage, err := os.MkdirTemp(parent, ".export-*")
	if err != nil {
		return report, err
	}
	defer os.RemoveAll(stage)
	payload := filepath.Join(stage, "payload")
	if err := os.Mkdir(payload, 0700); err != nil {
		return report, err
	}
	dst, err := os.OpenRoot(payload)
	if err != nil {
		return report, err
	}
	defer dst.Close()
	cacheRoot, err := os.OpenRoot(cache)
	if err != nil {
		return report, err
	}
	defer cacheRoot.Close()
	for _, a := range lock.Artifacts {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		name := "mods/" + a.FileID + ".jar"
		if err := localfs.TargetPath(payload, name); err != nil {
			return report, err
		}
		if err := dst.MkdirAll("mods", 0700); err != nil {
			return report, err
		}
		info, err := cacheRoot.Lstat(a.SHA512[7:])
		if err != nil {
			return report, document.Fail("MissingArtifact", a.FileID+": "+err.Error())
		}
		if !info.Mode().IsRegular() {
			return report, document.Fail("UnsupportedFile", "cache entry is not regular")
		}
		input, err := cacheRoot.Open(a.SHA512[7:])
		if err != nil {
			return report, err
		}
		output, err := dst.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			input.Close()
			return report, err
		}
		copyErr := copyVerified(ctx, input, output, a.Size, a.SHA512)
		input.Close()
		syncErr := output.Sync()
		closeErr := output.Close()
		if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
			return report, err
		}
	}
	if len(lock.Pack.Configs) > 0 {
		r, err := os.OpenRoot(configs)
		if err != nil {
			return report, err
		}
		defer r.Close()
		for _, c := range lock.Pack.Configs {
			if err := localfs.TargetPath(payload, c.Path); err != nil {
				return report, err
			}
			info, err := r.Lstat(c.Path)
			if err != nil {
				return report, err
			}
			if !info.Mode().IsRegular() {
				return report, document.Fail("UnsupportedFile", "config is not regular")
			}
			input, err := r.Open(c.Path)
			if err != nil {
				return report, err
			}
			if err := dst.MkdirAll(filepath.Dir(c.Path), 0700); err != nil {
				input.Close()
				return report, err
			}
			output, err := dst.OpenFile(c.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				input.Close()
				return report, document.Fail("FileCollision", err.Error())
			}
			copyErr := copyVerified(ctx, input, output, info.Size(), c.SHA256)
			input.Close()
			syncErr := output.Sync()
			closeErr := output.Close()
			if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
				return report, err
			}
		}
	}
	if err := localfs.Write(filepath.Join(payload, "modgrid.lock.json"), lockBytes, false); err != nil {
		return report, err
	}
	files, err := localfs.Scan(ctx, payload)
	if err != nil {
		return report, err
	}
	report = ExportManifest{Schema: "modgrid.export.v1", LockID: id, Files: files}
	if err := localfs.WriteJSON(filepath.Join(stage, "manifest.json"), report, false); err != nil {
		return report, err
	}
	if err := localfs.SyncTree(stage); err != nil {
		return report, err
	}
	// Windows does not permit renaming this stage while OpenRoot retains a
	// directory handle underneath it. Release that handle before publication.
	if err := dst.Close(); err != nil {
		return report, err
	}
	if err := localfs.Rename(stage, destination, false); err != nil {
		return report, err
	}
	return report, localfs.SyncDir(parent)
}

func VerifyExport(ctx context.Context, root string) (ExportManifest, error) {
	var m ExportManifest
	b, err := localfs.Read(filepath.Join(root, "manifest.json"))
	if err != nil {
		return m, err
	}
	if err := document.Decode(b, &m); err != nil {
		return m, err
	}
	if m.Schema != "modgrid.export.v1" {
		return m, document.Fail("UnsupportedSchema", "expected modgrid.export.v1")
	}
	if err := localfs.Verify(ctx, filepath.Join(root, "payload"), m.Files); err != nil {
		return m, err
	}
	b, err = localfs.Read(filepath.Join(root, "payload", "modgrid.lock.json"))
	if err != nil {
		return m, err
	}
	_, _, err = resolver.ReadLock(b, m.LockID)
	return m, err
}

// VerifyPayload checks locked mod/config bytes in a mutable world tree. World
// files are allowed to evolve, but extra mods cannot silently enter the profile.
func VerifyPayload(ctx context.Context, root string, lockBytes []byte) error {
	lock, _, err := resolver.ReadLock(lockBytes, "")
	if err != nil {
		return err
	}
	expected := map[string]bool{}
	for _, a := range lock.Artifacts {
		name := a.FileID + ".jar"
		expected[name] = true
		if err := VerifyFile(ctx, filepath.Join(root, "mods", name), a.Size, a.SHA512); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "mods"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, e := range entries {
		if !expected[e.Name()] || e.IsDir() {
			return document.Fail("UnmanagedMod", "extra/unmanaged mod in profile")
		}
	}
	for _, c := range lock.Pack.Configs {
		path := filepath.Join(root, filepath.FromSlash(c.Path))
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if err := VerifyFile(ctx, path, info.Size(), c.SHA256); err != nil {
			return err
		}
	}
	return nil
}
