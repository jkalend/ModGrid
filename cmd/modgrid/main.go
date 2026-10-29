package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"modgrid.local/modgrid/internal/buildinfo"
	"os"
	"path/filepath"
	"runtime"

	"modgrid.local/modgrid/internal/resolver"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		var diagnostic *resolver.Error
		if !errors.As(err, &diagnostic) {
			diagnostic = &resolver.Error{Code: "IOError", Message: err.Error(), Reasons: []string{}}
		}
		json.NewEncoder(os.Stderr).Encode(struct {
			Error *resolver.Error `json:"error"`
		}{diagnostic})
		os.Exit(1)
	}
}

func usageError(message string) error {
	return &resolver.Error{Code: "Usage", Message: message, Reasons: []string{}}
}

func run(args []string, stdout, stderr io.Writer) error {
	const usage = "usage: modgrid resolve --pack FILE --catalog FILE --out FILE [--json]\n       modgrid lock verify --lock FILE --pack FILE --catalog FILE [--expected-id sha256:HEX] [--json]\n       modgrid catalog id --catalog FILE"
	if len(args) == 0 {
		return usageError(usage)
	}
	if args[0] == "--version" || args[0] == "version" {
		return json.NewEncoder(stdout).Encode(struct {
			Version  string `json:"version"`
			Revision string `json:"revision"`
			Go       string `json:"go"`
			OS       string `json:"os"`
			Arch     string `json:"arch"`
		}{buildinfo.Version, buildinfo.Revision, runtime.Version(), runtime.GOOS, runtime.GOARCH})
	}
	if args[0] == "pack" || args[0] == "cache" || args[0] == "instance" {
		return localCommand(args, stdout, stderr)
	}
	if args[0] == "server" || args[0] == "runtime" {
		return serverCommand(args, stdout, stderr)
	}
	if args[0] == "--help" || args[0] == "-h" {
		_, err := fmt.Fprintln(stdout, usage+"\n       modgrid pack export/verify | cache add/verify | instance init/inspect/checkpoint/restore/rollback/pin/unpin/reconcile/gc\n       modgrid --version")
		return err
	}
	command, rest := args[0], args[1:]
	if command == "lock" || command == "catalog" {
		if len(rest) == 0 {
			return usageError(usage)
		}
		command += " " + rest[0]
		rest = rest[1:]
	}
	if command != "resolve" && command != "lock verify" && command != "catalog id" {
		return usageError(usage)
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	catalogPath := flags.String("catalog", "", "frozen CatalogSnapshot v1 JSON")
	var packPath, outPath, lockPath, expectedID string
	var machine bool
	if command != "catalog id" {
		flags.StringVar(&packPath, "pack", "", "PackSpec v1 JSON")
		flags.BoolVar(&machine, "json", false, "emit a JSON operation report")
	}
	if command == "resolve" {
		flags.StringVar(&outPath, "out", "", "output lock path")
	}
	if command == "lock verify" {
		flags.StringVar(&lockPath, "lock", "", "canonical Lockfile v1")
		flags.StringVar(&expectedID, "expected-id", "", "separately retained lock identity")
	}
	if err := flags.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return usageError(err.Error())
	}
	if flags.NArg() != 0 || *catalogPath == "" || (command != "catalog id" && packPath == "") ||
		(command == "resolve" && outPath == "") || (command == "lock verify" && lockPath == "") {
		return usageError(usage)
	}
	catalog, err := read(*catalogPath)
	if err != nil {
		return err
	}
	if command == "catalog id" {
		id, err := resolver.CatalogIdentity(catalog)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, id)
		return err
	}
	pack, err := read(packPath)
	if err != nil {
		return err
	}
	var lock resolver.Lock
	var id string
	if command == "resolve" {
		var b []byte
		lock, b, id, err = resolver.Resolve(pack, catalog)
		if err != nil {
			return err
		}
		if err := distinctOutput(outPath, packPath, *catalogPath); err != nil {
			return err
		}
		if err := writeLock(outPath, b); err != nil {
			return err
		}
	} else {
		b, err := read(lockPath)
		if err != nil {
			return err
		}
		lock, id, err = resolver.Verify(b, pack, catalog, expectedID)
		if err != nil {
			return err
		}
	}
	if machine {
		return json.NewEncoder(stdout).Encode(struct {
			Schema string        `json:"schema"`
			Action string        `json:"action"`
			LockID string        `json:"lock_id"`
			Lock   resolver.Lock `json:"lock"`
		}{"modgrid.report.v1", command, id, lock})
	}
	if _, err := fmt.Fprintln(stdout, command+": "+id); err != nil {
		return err
	}
	for _, a := range lock.Artifacts {
		if _, err := fmt.Fprintf(stdout, "  %s %s (%s)\n", a.Project, a.Version, a.FileID); err != nil {
			return err
		}
	}
	for _, r := range lock.Reasons {
		if _, err := fmt.Fprintf(stdout, "  %s requires %s %s\n", r.From, r.Project, r.Constraint); err != nil {
			return err
		}
	}
	return nil
}

func read(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, &resolver.Error{Code: "SourceUnavailable", Message: err.Error(), Reasons: []string{}}
	}
	defer f.Close()
	return resolver.ReadDocument(f)
}

func distinctOutput(output string, inputs ...string) error {
	out, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	info, err := os.Lstat(out)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if info != nil && !info.Mode().IsRegular() {
		return usageError("output must be a regular file, not a link/directory")
	}
	for _, input := range inputs {
		in, err := filepath.Abs(input)
		if err != nil {
			return err
		}
		inputInfo, err := os.Stat(in)
		if err != nil {
			return err
		}
		if out == in || (info != nil && os.SameFile(info, inputInfo)) {
			return usageError("output must differ from PackSpec and catalog inputs")
		}
	}
	return nil
}

// Publish only after a successful solve and a complete synced temporary write.
// This is not a lifecycle transaction or a power-loss durability guarantee.
func writeLock(path string, b []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".modgrid-lock-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
