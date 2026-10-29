package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"modgrid.local/modgrid/internal/document"
	"modgrid.local/modgrid/internal/instance"
	"modgrid.local/modgrid/internal/localfs"
	"modgrid.local/modgrid/internal/pack"
	"modgrid.local/modgrid/internal/resolver"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Options struct {
	Address          string
	Marker           int
	InitializeMarker bool
	CheckAndStop     bool
	StartupTimeout   time.Duration
	StopTimeout      time.Duration
}
type RunReport struct {
	Schema         string `json:"schema"`
	LockID         string `json:"lock_id"`
	RuntimeID      string `json:"runtime_id"`
	WorkID         string `json:"working_id"`
	StatusVerified bool   `json:"status_verified"`
	MarkerVerified bool   `json:"marker_verified"`
	CleanStop      bool   `json:"clean_stop"`
	LogBytes       int64  `json:"log_bytes"`
}

func property(b []byte, key string) (string, error) {
	value := ""
	found := false
	for _, c := range b {
		if c > 127 {
			return "", document.Fail("UnsupportedConfiguration", "this server-properties profile requires ASCII (Java escapes may be used in unrelated values)")
		}
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.Trim(line, " \t\r\f")
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		slashes := 0
		for i := len(line) - 1; i >= 0 && line[i] == '\\'; i-- {
			slashes++
		}
		if slashes%2 == 1 {
			return "", document.Fail("UnsupportedConfiguration", "continued property lines are unsupported")
		}
		separator := strings.IndexAny(line, "=: \t\f")
		if separator < 0 {
			continue
		}
		propertyKey := line[:separator]
		if strings.Contains(propertyKey, "\\") {
			return "", document.Fail("UnsupportedConfiguration", "escaped property keys are unsupported")
		}
		rest := strings.TrimLeft(line[separator:], " \t\f")
		if strings.HasPrefix(rest, "=") || strings.HasPrefix(rest, ":") {
			rest = rest[1:]
		}
		rest = strings.Trim(rest, " \t\r\f")
		if propertyKey == key {
			if strings.Contains(rest, "\\") {
				return "", document.Fail("UnsupportedConfiguration", "escaped network/EULA property values are unsupported")
			}
			if found {
				return "", document.Fail("InvalidConfiguration", "duplicate property "+key)
			}
			found = true
			value = rest
		}
	}
	if !found {
		return "", document.Fail("InvalidConfiguration", "missing property "+key)
	}
	return value, nil
}

func Run(ctx context.Context, store instance.Store, java string, expected int64, options Options, input io.Reader, logs io.Writer) (RunReport, error) {
	report := RunReport{Schema: "modgrid.run.v1"}
	work, err := store.BeginRun(ctx, expected)
	if err != nil {
		return report, err
	}
	report.WorkID = work.ID
	root := store.WorkingPath(work)
	runtimeBytes, err := localfs.Read(filepath.Join(root, "modgrid.runtime.json"))
	if err != nil {
		store.FinishRun(context.Background(), work.ID, false, false)
		return report, err
	}
	var rt Runtime
	if err := document.Decode(runtimeBytes, &rt); err != nil {
		store.FinishRun(context.Background(), work.ID, false, false)
		return report, err
	}
	report, err = runProcess(ctx, root, java, rt, options, input, logs)
	report.WorkID = work.ID
	finishCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	finishErr := store.FinishRun(finishCtx, work.ID, report.CleanStop, report.MarkerVerified)
	return report, errors.Join(err, finishErr)
}

func runProcess(ctx context.Context, root, java string, rt Runtime, o Options, input io.Reader, logs io.Writer) (RunReport, error) {
	report := RunReport{Schema: "modgrid.run.v1"}
	if o.StartupTimeout <= 0 {
		o.StartupTimeout = 120 * time.Second
	}
	if o.StopTimeout <= 0 {
		o.StopTimeout = 30 * time.Second
	}
	if o.Marker < 0 || o.Marker > 2147483647 {
		return report, document.Fail("InvalidInput", "marker must fit a nonnegative scoreboard integer")
	}
	if err := VerifyRuntime(ctx, root, java, rt); err != nil {
		return report, err
	}
	lockBytes, err := localfs.Read(filepath.Join(root, "modgrid.lock.json"))
	if err != nil {
		return report, err
	}
	if err := pack.VerifyPayload(ctx, root, lockBytes); err != nil {
		return report, err
	}
	locked, lockID, err := resolver.ReadLock(lockBytes, "")
	if err != nil {
		return report, err
	}
	if locked.Pack.Target.Side != "server" || locked.Pack.Target.Minecraft != rt.Minecraft || locked.Pack.Target.Loader != rt.Loader || locked.Pack.Target.JavaMajor != rt.JavaMajor || locked.Pack.Target.Platform != rt.OS+"/"+rt.Arch {
		return report, document.Fail("RuntimeMismatch", "runtime and lock target differ")
	}
	report.LockID = lockID
	runtimeBytes, err := document.Canonical(rt)
	if err != nil {
		return report, err
	}
	report.RuntimeID = document.Digest(runtimeBytes)
	eula, err := localfs.Read(filepath.Join(root, "eula.txt"))
	if err != nil {
		return report, document.Fail("EULARequired", "operator-provided eula.txt with eula=true is required")
	}
	accepted, err := property(eula, "eula")
	if err != nil || accepted != "true" {
		return report, document.Fail("EULARequired", "Minecraft EULA must be explicitly accepted by the operator")
	}
	properties, err := localfs.Read(filepath.Join(root, "server.properties"))
	if err != nil {
		return report, err
	}
	ip, err := property(properties, "server-ip")
	if err != nil || ip != "127.0.0.1" {
		return report, document.Fail("UnsupportedNetwork", "supported server profile binds explicitly to 127.0.0.1")
	}
	port, err := property(properties, "server-port")
	if err != nil {
		return report, err
	}
	if o.Address != "127.0.0.1:"+port {
		return report, document.Fail("InvalidConfiguration", "status endpoint must match the loopback server properties")
	}
	for key, want := range map[string]string{"enable-rcon": "false", "white-list": "true", "enforce-whitelist": "true"} {
		value, err := property(properties, key)
		if err != nil || value != want {
			return report, document.Fail("UnsupportedNetwork", "required property "+key+"="+want)
		}
	}
	command := exec.Command(java, "-Xms256M", "-Xmx1G", "-jar", rt.Entry, "nogui")
	command.Dir = root
	command.Env = []string{"HOME=" + root}
	for _, key := range []string{"SystemRoot", "WINDIR", "TEMP", "TMP"} {
		if value, ok := os.LookupEnv(key); ok {
			command.Env = append(command.Env, key+"="+value)
		}
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		return report, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return report, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return report, err
	}
	if err := command.Start(); err != nil {
		return report, err
	}
	defer stdin.Close()
	lines := make(chan string, 64)
	readErrors := make(chan error, 2)
	done := make(chan struct{})
	defer close(done)
	var readers sync.WaitGroup
	for _, stream := range []io.Reader{stdout, stderr} {
		readers.Add(1)
		go func(r io.Reader) {
			defer readers.Done()
			scanner := bufio.NewScanner(r)
			scanner.Buffer(make([]byte, 4096), 65536)
			for scanner.Scan() {
				select {
				case lines <- scanner.Text():
				case <-done:
					return
				}
			}
			if err := scanner.Err(); err != nil {
				select {
				case readErrors <- err:
				case <-done:
				}
			}
		}(stream)
	}
	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()
	commands := make(chan string, 8)
	if input != nil {
		go func() {
			scanner := bufio.NewScanner(input)
			scanner.Buffer(make([]byte, 4096), 4096)
			for scanner.Scan() {
				select {
				case commands <- scanner.Text():
				case <-done:
					return
				}
			}
			select {
			case commands <- "stop":
			case <-done:
			}
		}()
	}
	startup := time.NewTimer(o.StartupTimeout)
	defer startup.Stop()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	var stopDeadline <-chan time.Time
	var stopTimer *time.Timer
	stopping := false
	assertSent := false
	ctxDone := ctx.Done()
	nonce := localfs.ID()
	var failure error
	send := func(line string) error { _, err := io.WriteString(stdin, line+"\n"); return err }
	stop := func(reason error) {
		if stopping {
			return
		}
		failure = reason
		stopping = true
		_ = send("stop")
		stopTimer = time.NewTimer(o.StopTimeout)
		stopDeadline = stopTimer.C
	}
	defer func() {
		if stopTimer != nil {
			stopTimer.Stop()
		}
	}()
	for {
		select {
		case line := <-lines:
			report.LogBytes += int64(len(line) + 1)
			if report.LogBytes > 4<<20 {
				stop(document.Fail("ResourceLimit", "console exceeds 4 MiB per run"))
				continue
			}
			if logs != nil {
				if _, err := fmt.Fprintln(logs, line); err != nil {
					stop(err)
				}
			}
			if assertSent && strings.Contains(line, "MODGRID_MARKER_"+nonce) {
				report.MarkerVerified = true
				startup.Stop()
				if o.CheckAndStop {
					stop(nil)
				}
			}
		case err := <-readErrors:
			if !stopping {
				stop(err)
			}
		case <-tick.C:
			if stopping || report.StatusVerified {
				continue
			}
			statusCtx, cancel := context.WithTimeout(ctx, time.Second)
			status, err := Ping(statusCtx, o.Address)
			cancel()
			if err != nil {
				continue
			}
			if status.Version.Name != rt.Minecraft {
				stop(document.Fail("InvalidStatus", "server reported a different game version"))
				continue
			}
			report.StatusVerified = true
			if o.Marker > 0 {
				if o.InitializeMarker {
					if err := send("scoreboard objectives add modgrid dummy"); err != nil {
						stop(err)
						continue
					}
					if err := send("scoreboard players set marker modgrid " + strconv.Itoa(o.Marker)); err != nil {
						stop(err)
						continue
					}
				}
				if err := send("execute if score marker modgrid matches " + strconv.Itoa(o.Marker) + " run say MODGRID_MARKER_" + nonce); err != nil {
					stop(err)
					continue
				}
				assertSent = true
			} else if o.CheckAndStop {
				startup.Stop()
				stop(nil)
			} else if o.Marker == 0 {
				startup.Stop()
			}
		case line := <-commands:
			if line == "stop" {
				stop(nil)
			} else if !stopping {
				if err := send(line); err != nil {
					stop(err)
				}
			}
		case <-startup.C:
			if !stopping {
				stop(document.Fail("StartupTimeout", "server status/world marker was not confirmed before the deadline"))
			}
		case <-ctxDone:
			stop(ctx.Err())
			ctxDone = nil
		case <-stopDeadline:
			_ = command.Process.Kill()
			select {
			case <-exited:
			case <-time.After(10 * time.Second):
			}
			return report, document.Fail("UnconfirmedStop", "graceful stop timed out; working tree/lease retained")
		case exitErr := <-exited:
			if !stopping || exitErr != nil {
				return report, document.Fail("UncleanExit", "server exited without a confirmed graceful stop")
			}
			report.CleanStop = true
			if o.CheckAndStop && (!report.StatusVerified || (o.Marker > 0 && !report.MarkerVerified)) {
				return report, document.Fail("ValidationFailed", "status or world marker validation failed")
			}
			return report, failure
		}
	}
}
