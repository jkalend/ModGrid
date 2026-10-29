package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"modgrid.local/modgrid/internal/instance"
	"modgrid.local/modgrid/internal/localfs"
	"modgrid.local/modgrid/internal/server"
	"os"
	"os/signal"
)

func serverCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) < 2 {
		return usageError("expected runtime capture/attach or server run/status")
	}
	command := args[0] + " " + args[1]
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(stderr)
	root := f.String("root", "", "instance or template root")
	java := f.String("java", "", "explicit pinned Java executable")
	var entry, build, dist, out, export, template, address string
	var expected int64
	var marker int
	var trust, initialize, check bool
	switch command {
	case "runtime capture":
		f.StringVar(&entry, "entry", "fabric-server-launch.jar", "pinned launch entry")
		f.StringVar(&build, "build", "", "exact Java vendor/build")
		f.StringVar(&dist, "distribution-sha256", "", "pinned Java distribution digest")
		f.StringVar(&out, "out", "", "runtime profile output")
	case "runtime attach":
		f.StringVar(&export, "export", "", "verified pack export")
		f.StringVar(&template, "template", "", "prepared runtime template")
		f.StringVar(&out, "out", "", "fresh export destination")
	case "server status":
		f.StringVar(&address, "address", "127.0.0.1:25565", "loopback endpoint")
	case "server run":
		f.StringVar(&address, "address", "127.0.0.1:25565", "loopback endpoint")
		f.Int64Var(&expected, "expected-sequence", 0, "reviewed sequence")
		f.IntVar(&marker, "marker", 0, "expected saved scoreboard integer")
		f.BoolVar(&initialize, "initialize-marker", false, "explicitly write the initial world marker")
		f.BoolVar(&check, "check-and-stop", false, "validate status/marker and stop")
		f.BoolVar(&trust, "trust-code", false, "reviewed exact Java/mod identities; execute with this OS account's privileges")
	default:
		return usageError("unknown command " + command)
	}
	if err := f.Parse(args[2:]); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return usageError("unexpected positional arguments")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	var result any
	var err error
	switch command {
	case "runtime capture":
		if *root == "" || *java == "" || out == "" {
			return usageError("required --root, --java, --out, --build and --distribution-sha256")
		}
		result, err = server.Capture(*root, *java, entry, build, dist)
		if err == nil {
			err = localfs.WriteJSON(out, result, false)
		}
	case "runtime attach":
		if export == "" || template == "" || *java == "" || out == "" {
			return usageError("required --export, --template, --java and --out")
		}
		result, err = server.Attach(ctx, export, template, *java, out)
	case "server status":
		result, err = server.Ping(ctx, address)
	case "server run":
		if !trust || *root == "" || *java == "" {
			return usageError("required --root, --java, --expected-sequence and explicit --trust-code")
		}
		var input io.Reader = os.Stdin
		if check {
			input = nil
		}
		result, err = server.Run(ctx, instance.Store{Root: *root}, *java, expected, server.Options{Address: address, Marker: marker, InitializeMarker: initialize, CheckAndStop: check}, input, stderr)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}
