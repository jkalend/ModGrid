// Package server supervises one explicitly selected Java process. It provides
// process ownership and a world oracle, not a sandbox for executable mod code.
package server

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"io"
	"io/fs"
	"modgrid.local/modgrid/internal/document"
	"modgrid.local/modgrid/internal/localfs"
	"modgrid.local/modgrid/internal/pack"
	"modgrid.local/modgrid/internal/resolver"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type RuntimeFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size_bytes"`
	SHA512 string `json:"sha512"`
}
type Runtime struct {
	Schema             string          `json:"schema"`
	Minecraft          string          `json:"minecraft"`
	Loader             resolver.Loader `json:"loader"`
	JavaMajor          int             `json:"java_major"`
	OS                 string          `json:"os"`
	Arch               string          `json:"arch"`
	JavaBuild          string          `json:"java_build"`
	DistributionSHA256 string          `json:"distribution_sha256"`
	JavaSHA512         string          `json:"java_sha512"`
	Entry              string          `json:"entry"`
	Files              []RuntimeFile   `json:"files"`
}

func fileHash(path string) (int64, string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, "", err
	}
	if !info.Mode().IsRegular() || info.Size() > localfs.MaxFileBytes {
		return 0, "", document.Fail("UnsupportedFile", "runtime input must be a bounded regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha512.New()
	n, err := io.Copy(h, io.LimitReader(f, localfs.MaxFileBytes+1))
	if err != nil {
		return 0, "", err
	}
	if n != info.Size() {
		return 0, "", document.Fail("SourceChanged", "runtime file changed")
	}
	return n, "sha512:" + hex.EncodeToString(h.Sum(nil)), nil
}

func Capture(root, java, entry, build, distributionSHA256 string) (Runtime, error) {
	r := Runtime{Schema: "modgrid.runtime.v1", Minecraft: "1.21.1", Loader: resolver.Loader{ID: "fabric", Version: "0.19.5"}, JavaMajor: 21, OS: runtime.GOOS, Arch: runtime.GOARCH, JavaBuild: build, DistributionSHA256: distributionSHA256, Entry: entry, Files: []RuntimeFile{}}
	if _, err := localfs.TreePath(entry); err != nil {
		return r, err
	}
	if !strings.HasPrefix(distributionSHA256, "sha256:") || len(distributionSHA256) != 71 || build == "" {
		return r, document.Fail("InvalidRuntime", "exact Java build and distribution SHA-256 are required")
	}
	_, hash, err := fileHash(java)
	if err != nil {
		return r, err
	}
	r.JavaSHA512 = hash
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "." {
			return nil
		}
		include := relative == entry || strings.HasPrefix(relative, "libraries/") || strings.HasPrefix(relative, "versions/") || strings.HasPrefix(relative, ".fabric/")
		if d.IsDir() {
			if relative == "libraries" || relative == "versions" || relative == ".fabric" || include {
				return nil
			}
			return filepath.SkipDir
		}
		if !include {
			return nil
		}
		if _, err := localfs.TreePath(relative); err != nil {
			return err
		}
		size, hash, err := fileHash(path)
		if err != nil {
			return err
		}
		r.Files = append(r.Files, RuntimeFile{Path: relative, Size: size, SHA512: hash})
		return nil
	})
	sort.Slice(r.Files, func(i, j int) bool { return r.Files[i].Path < r.Files[j].Path })
	return r, err
}

func VerifyRuntime(ctx context.Context, root, java string, r Runtime) error {
	if r.Schema != "modgrid.runtime.v1" || r.OS != runtime.GOOS || r.Arch != runtime.GOARCH || r.JavaMajor != 21 || r.Minecraft != "1.21.1" || r.Loader.ID != "fabric" || len(r.Files) == 0 || len(r.Files) > 1000 {
		return document.Fail("UnsupportedRuntime", "runtime schema/platform/target is unsupported")
	}
	info, err := os.Stat(java)
	if err != nil {
		return err
	}
	if err := pack.VerifyFile(ctx, java, info.Size(), r.JavaSHA512); err != nil {
		return err
	}
	seen := map[string]bool{}
	entry := false
	for _, f := range r.Files {
		key, err := localfs.TreePath(f.Path)
		if err != nil {
			return err
		}
		if seen[key] {
			return document.Fail("InvalidRuntime", "duplicate runtime path")
		}
		seen[key] = true
		if f.Path == r.Entry {
			entry = true
		}
		if err := pack.VerifyFile(ctx, filepath.Join(root, filepath.FromSlash(f.Path)), f.Size, f.SHA512); err != nil {
			return err
		}
	}
	if !entry {
		return document.Fail("InvalidRuntime", "launch entry is not pinned")
	}
	return nil
}
