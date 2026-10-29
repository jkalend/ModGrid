// package builds a deterministic, self-contained ModGrid distribution. It never
// includes a user's worlds, configuration, caches, or third-party game binaries.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"modgrid.local/modgrid/internal/document"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

type module struct {
	Path    string
	Version string
	Dir     string
	Main    bool
}

func main() {
	if err := build(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func build() error {
	version := flag.String("version", "0.1.0-dev", "artifact version")
	out := flag.String("out", "dist", "artifact directory")
	targetOS := flag.String("os", runtime.GOOS, "windows or linux")
	flag.Parse()
	if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.-]+)?$`).MatchString(*version) || (*targetOS != "windows" && *targetOS != "linux") {
		return fmt.Errorf("invalid version or target OS")
	}
	revisionBytes, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return err
	}
	revision := strings.TrimSpace(string(revisionBytes))
	if err := os.MkdirAll(*out, 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp("", "modgrid-package-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	binaryName := "modgrid"
	if *targetOS == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(stage, binaryName)
	command := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags", "-s -w -buildid= -X modgrid.local/modgrid/internal/buildinfo.Version="+*version+" -X modgrid.local/modgrid/internal/buildinfo.Revision="+revision, "-o", binaryPath, "./cmd/modgrid")
	command.Env = append(os.Environ(), "GOOS="+*targetOS, "GOARCH=amd64", "CGO_ENABLED=0")
	if b, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("build: %s %w", b, err)
	}
	files := map[string][]byte{}
	for _, name := range []string{"LICENSE", "README.md", "THIRD_PARTY_NOTICES.md", "go.mod", "go.sum"} {
		b, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		files[name] = b
	}
	files[binaryName], err = os.ReadFile(binaryPath)
	if err != nil {
		return err
	}
	for _, root := range []string{"docs", "tests/fixtures/offline"} {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			name := filepath.ToSlash(path)
			if strings.HasPrefix(name, "tests/fixtures/") {
				name = "examples/" + strings.TrimPrefix(name, "tests/fixtures/")
			}
			files[name] = b
			return nil
		})
		if err != nil {
			return err
		}
	}
	listing, err := exec.Command("go", "list", "-deps", "-json", "./cmd/modgrid").Output()
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(listing))
	components := []any{}
	seenModules := map[string]bool{}
	for {
		var dependency struct{ Module *module }
		err := decoder.Decode(&dependency)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if dependency.Module == nil || dependency.Module.Main || seenModules[dependency.Module.Path] {
			continue
		}
		m := *dependency.Module
		seenModules[m.Path] = true
		components = append(components, map[string]any{"type": "library", "name": m.Path, "version": m.Version, "purl": "pkg:golang/" + m.Path + "@" + m.Version})
		b, err := os.ReadFile(filepath.Join(m.Dir, "LICENSE"))
		if err != nil {
			return fmt.Errorf("missing dependency license %s: %w", m.Path, err)
		}
		files["licenses/"+strings.NewReplacer("/", "_", "!", "").Replace(m.Path)+"-LICENSE"] = b
	}
	goRoot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return err
	}
	files["licenses/go-LICENSE"], err = os.ReadFile(filepath.Join(strings.TrimSpace(string(goRoot)), "LICENSE"))
	if err != nil {
		return err
	}
	if b, err := os.ReadFile("third_party/Apache-2.0.txt"); err == nil {
		files["licenses/Apache-2.0.txt"] = b
	} else {
		return err
	}
	sbom := map[string]any{"bomFormat": "CycloneDX", "specVersion": "1.6", "version": 1, "metadata": map[string]any{"component": map[string]any{"type": "application", "name": "ModGrid", "version": *version}}, "components": components}
	files["SBOM.cdx.json"], err = document.Canonical(sbom)
	if err != nil {
		return err
	}
	evidence := map[string]any{"schema": "modgrid.build.v1", "version": *version, "revision": revision, "go": runtime.Version(), "os": *targetOS, "arch": "amd64", "qualification": "development artifact; full 1.0 runtime/client/stability release gates are separate"}
	files["BUILD.json"], err = document.Canonical(evidence)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	archiveName := "modgrid_" + *version + "_" + *targetOS + "_amd64.zip"
	archivePath := filepath.Join(*out, archiveName)
	f, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	z := zip.NewWriter(f)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		header.SetMode(0644)
		if name == binaryName {
			header.SetMode(0755)
		}
		w, err := z.CreateHeader(header)
		if err != nil {
			f.Close()
			return err
		}
		if _, err := w.Write(files[name]); err != nil {
			f.Close()
			return err
		}
	}
	if err := z.Close(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if *targetOS == runtime.GOOS {
		if err := smoke(archivePath, binaryName); err != nil {
			return err
		}
	}
	b, err := os.ReadFile(archivePath)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(b)
	if err := os.WriteFile(filepath.Join(*out, archiveName+".sha256"), []byte(hex.EncodeToString(hash[:])+"  "+archiveName+"\n"), 0600); err != nil {
		return err
	}
	fmt.Println(archivePath)
	return nil
}

func smoke(archivePath, binary string) error {
	root, err := os.MkdirTemp("", "modgrid-smoke-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	z, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer z.Close()
	for _, entry := range z.File {
		target := filepath.Join(root, filepath.FromSlash(entry.Name))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		r, err := entry.Open()
		if err != nil {
			return err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, entry.Mode())
		if err != nil {
			r.Close()
			return err
		}
		_, copyErr := io.Copy(f, r)
		r.Close()
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	cli := filepath.Join(root, binary)
	for _, args := range [][]string{{"--help"}, {"--version"}, {"resolve", "--pack", "examples/offline/pack.json", "--catalog", "examples/offline/catalog.json", "--out", "smoke.lock.json"}, {"lock", "verify", "--lock", "smoke.lock.json", "--pack", "examples/offline/pack.json", "--catalog", "examples/offline/catalog.json"}} {
		command := exec.Command(cli, args...)
		command.Dir = root
		if b, err := command.CombinedOutput(); err != nil {
			return fmt.Errorf("packaged smoke %v: %s %w", args, b, err)
		}
	}
	return nil
}
