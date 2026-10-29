package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"modgrid.local/modgrid/internal/instance"
	"modgrid.local/modgrid/internal/pack"
	"path/filepath"
)

func localCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) < 2 {
		return usageError("expected pack export/verify, cache add/verify, or instance init/inspect/checkpoint/restore/rollback/pin/unpin/reconcile/gc")
	}
	command := args[0] + " " + args[1]
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(stderr)
	var root, lock, cache, configs, out, file, digest, generation, reference string
	var size, expected int64
	var apply, confirm bool
	str := func(name string, target *string) { f.StringVar(target, name, "", name) }
	str("root", &root)
	switch command {
	case "pack export":
		str("lock", &lock)
		str("cache", &cache)
		str("configs", &configs)
		str("out", &out)
	case "pack verify":
	case "cache add", "cache verify":
		str("file", &file)
		str("cache", &cache)
		str("digest", &digest)
		f.Int64Var(&size, "size", -1, "exact size")
	case "instance init":
		str("export", &reference)
	case "instance inspect":
	case "instance checkpoint":
		f.Int64Var(&expected, "expected-sequence", 0, "reviewed selector sequence")
	case "instance restore":
		str("generation", &generation)
		str("out", &out)
		f.Int64Var(&expected, "expected-sequence", 0, "reviewed selector sequence")
	case "instance rollback":
		str("generation", &generation)
		f.Int64Var(&expected, "expected-sequence", 0, "reviewed selector sequence")
		f.BoolVar(&confirm, "confirm-loss", false, "accept loss of writes after the checkpoint")
	case "instance pin", "instance unpin":
		str("generation", &generation)
		f.Int64Var(&expected, "expected-sequence", 0, "reviewed selector sequence")
	case "instance reconcile":
		str("lease", &reference)
		f.Int64Var(&expected, "expected-sequence", 0, "reviewed selector sequence")
		f.BoolVar(&confirm, "confirm-stopped", false, "operator confirms original process/use is stopped")
	case "instance gc":
		f.Int64Var(&expected, "expected-sequence", 0, "reviewed selector sequence")
		f.BoolVar(&apply, "apply", false, "apply the collection preview")
	default:
		return usageError("unknown command: " + command)
	}
	if err := f.Parse(args[2:]); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return usageError("unexpected positional arguments")
	}
	ctx := context.Background()
	store := instance.Store{Root: root}
	var result any
	var err error
	require := func(value, name string) error {
		if value == "" {
			return usageError("required --" + name)
		}
		return nil
	}
	if args[0] == "instance" || command == "pack verify" {
		if err := require(root, "root"); err != nil {
			return err
		}
	}
	switch command {
	case "pack export":
		for name, value := range map[string]string{"lock": lock, "cache": cache, "out": out} {
			if err := require(value, name); err != nil {
				return err
			}
		}
		b, e := read(lock)
		if e != nil {
			return e
		}
		result, err = pack.Export(ctx, b, cache, configs, out)
	case "pack verify":
		result, err = pack.VerifyExport(ctx, root)
	case "cache add", "cache verify":
		if file == "" || digest == "" || size < 0 {
			return usageError("required --file, --digest and nonnegative --size")
		}
		if command == "cache add" {
			if cache == "" {
				return usageError("required --cache")
			}
			err = pack.Put(ctx, file, cache, size, digest)
		} else {
			err = pack.VerifyFile(ctx, file, size, digest)
		}
		result = struct {
			Digest   string `json:"digest"`
			Verified bool   `json:"verified"`
		}{digest, err == nil}
	case "instance init":
		if reference == "" {
			return usageError("required --export")
		}
		if _, err = pack.VerifyExport(ctx, reference); err == nil {
			result, err = store.Initialize(ctx, filepath.Join(reference, "payload"))
		}
	case "instance inspect":
		result, err = store.Inspect(ctx)
	case "instance checkpoint":
		result, err = store.Checkpoint(ctx, expected)
	case "instance restore":
		if generation == "" || out == "" {
			return usageError("required --generation and --out")
		}
		result, err = store.Restore(ctx, generation, out, expected)
	case "instance rollback":
		if !confirm {
			return usageError("rollback loses newer writes; require --confirm-loss")
		}
		result, err = store.Rollback(ctx, generation, expected)
	case "instance pin", "instance unpin":
		result, err = store.Pin(ctx, generation, command == "instance pin", expected)
	case "instance reconcile":
		result, err = store.Reconcile(ctx, reference, confirm, expected)
	case "instance gc":
		result, err = store.GC(ctx, expected, apply)
	}
	if err != nil {
		return err
	}
	if result == nil {
		return fmt.Errorf("command produced no result")
	}
	return json.NewEncoder(stdout).Encode(struct {
		Schema string `json:"schema"`
		Action string `json:"action"`
		Result any    `json:"result"`
	}{"modgrid.operation.v1", command, result})
}
