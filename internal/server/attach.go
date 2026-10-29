package server

import (
	"context"
	"errors"
	"io"
	"modgrid.local/modgrid/internal/document"
	"modgrid.local/modgrid/internal/localfs"
	"modgrid.local/modgrid/internal/pack"
	"os"
	"path/filepath"
)

// Attach creates a new export, retaining the operator's supplied exact runtime
// and EULA/settings. It never modifies the existing export or accepts an EULA.
func Attach(ctx context.Context, export, template, java, destination string) (pack.ExportManifest, error) {
	m, err := pack.VerifyExport(ctx, export)
	if err != nil {
		return m, err
	}
	b, err := localfs.Read(filepath.Join(template, "modgrid.runtime.json"))
	if err != nil {
		return m, err
	}
	var rt Runtime
	if err := document.Decode(b, &rt); err != nil {
		return m, err
	}
	if err := VerifyRuntime(ctx, template, java, rt); err != nil {
		return m, err
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return m, err
	}
	stage, err := os.MkdirTemp(parent, ".runtime-*")
	if err != nil {
		return m, err
	}
	defer os.RemoveAll(stage)
	payload := filepath.Join(stage, "payload")
	if _, err := localfs.CopyFresh(ctx, filepath.Join(export, "payload"), payload); err != nil {
		return m, err
	}
	files := append([]RuntimeFile{}, rt.Files...)
	for _, name := range []string{"server.properties", "eula.txt", "modgrid.runtime.json"} {
		size, h, err := fileHash(filepath.Join(template, name))
		if err != nil {
			return m, err
		}
		files = append(files, RuntimeFile{Path: name, Size: size, SHA512: h})
	}
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return m, err
		}
		if err := localfs.TargetPath(payload, f.Path); err != nil {
			return m, err
		}
		target := filepath.Join(payload, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return m, err
		}
		in, err := os.Open(filepath.Join(template, filepath.FromSlash(f.Path)))
		if err != nil {
			return m, err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			in.Close()
			return m, document.Fail("FileCollision", err.Error())
		}
		_, copyErr := io.Copy(out, io.LimitReader(in, f.Size+1))
		in.Close()
		syncErr := out.Sync()
		closeErr := out.Close()
		if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
			return m, err
		}
		if err := pack.VerifyFile(ctx, target, f.Size, f.SHA512); err != nil {
			return m, err
		}
	}
	m.Files, err = localfs.Scan(ctx, payload)
	if err != nil {
		return m, err
	}
	if err := localfs.WriteJSON(filepath.Join(stage, "manifest.json"), m, false); err != nil {
		return m, err
	}
	if err := localfs.SyncTree(stage); err != nil {
		return m, err
	}
	if err := localfs.Rename(stage, destination, false); err != nil {
		return m, err
	}
	return m, localfs.SyncDir(parent)
}
