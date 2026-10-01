// Copyright 2026 RetailNext, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package main is the entry point and composition root of cassandrasafe.
//
// cassandrasafe runs next to a Cassandra node. It exits with code 0 when every
// peer in the datacenter of that node answers CQL queries a required number of
// times in a row.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
	"github.com/retailnext/cassandrasafe/cassandra"
	"github.com/retailnext/cassandrasafe/node"
	"github.com/retailnext/cassandrasafe/peercheck"
	"github.com/retailnext/cassandrasafe/peerdiscovery"
	"github.com/retailnext/cassandrasafe/steadystate"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2

	formatAuto = "auto"
	formatJSON = "json"
	formatText = "text"

	keyError          = "error"
	keyHost           = "host"
	keyPeers          = "peers"
	keyRequiredPasses = "required_passes"
)

// CLI holds the command line flags.
type CLI struct {
	Host           string           `default:"127.0.0.1" help:"Host and optional port of the Cassandra node to query first."`
	RequiredPasses int              `default:"6" help:"Number of consecutive passed checks that lets the program exit."`
	PassInterval   time.Duration    `default:"5s" help:"Pause after a passed check."`
	RetryInterval  time.Duration    `default:"1s" help:"Pause before the program queries a host again after a failure."`
	StatusInterval time.Duration    `default:"1s" help:"Pause between log lines that list the hosts that did not answer yet."`
	LogFormat      string           `default:"auto" enum:"auto,text,json" help:"Log format. The value auto selects text on a terminal and json otherwise."`
	LogLevel       string           `default:"info" enum:"debug,info,warn,error" help:"Lowest log level to write."`
	Version        kong.VersionFlag `help:"Print the version and exit."`
}

// discoverer finds the peers to check.
type discoverer interface {
	Discover(ctx context.Context, host string) (node.TokensByHost, error)
}

// checker runs one check of all peers.
type checker interface {
	Check(ctx context.Context, tokensByHost node.TokensByHost) error
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := execute(ctx, os.Args[1:], os.Stdout, os.Stderr, os.Exit)
	stop()
	os.Exit(code)
}

// execute parses args, wires the components, and runs the program. It writes
// errors to stderr and returns the exit code.
func execute(ctx context.Context, args []string, stdout io.Writer, stderr *os.File, exit func(int)) int {
	cli, err := parseCLI(args, stdout, exit)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return exitUsage
	}
	logger, err := newLogger(stderr, isTerminal(stderr), cli.LogFormat, cli.LogLevel)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return exitUsage
	}

	client := cassandra.New(logger, cassandra.Options{})
	d := peerdiscovery.New(logger, client, cli.RetryInterval)
	c := peercheck.New(logger, client, cli.RetryInterval, cli.StatusInterval)
	if err := run(ctx, logger, cli, d, c); err != nil {
		logger.ErrorContext(ctx, "exiting with error", slog.Any(keyError, err))
		return exitError
	}
	return exitOK
}

// isTerminal reports whether f is a character device, such as a terminal.
func isTerminal(f *os.File) bool {
	stat, err := f.Stat()
	return err == nil && stat.Mode()&os.ModeCharDevice != 0
}

// parseCLI parses args. Help and version requests write to stdout and call
// exit with code 0.
func parseCLI(args []string, stdout io.Writer, exit func(int)) (CLI, error) {
	var cli CLI
	parser, err := kong.New(&cli,
		kong.Name("cassandrasafe"),
		kong.Description("Wait until every Cassandra peer in the local datacenter answers queries."),
		kong.Writers(stdout, stdout),
		kong.Exit(exit),
		kong.Vars{"version": buildVersion()},
	)
	if err != nil {
		return CLI{}, fmt.Errorf("build command line parser: %w", err)
	}
	if _, err := parser.Parse(args); err != nil {
		return CLI{}, fmt.Errorf("parse command line: %w", err)
	}
	if cli.RequiredPasses < 1 {
		return CLI{}, fmt.Errorf("parse command line: --required-passes must be 1 or more, got %d", cli.RequiredPasses)
	}
	if cli.RetryInterval <= 0 {
		return CLI{}, fmt.Errorf("parse command line: --retry-interval must be more than 0, got %s", cli.RetryInterval)
	}
	if cli.StatusInterval <= 0 {
		return CLI{}, fmt.Errorf("parse command line: --status-interval must be more than 0, got %s", cli.StatusInterval)
	}
	if cli.PassInterval < 0 {
		return CLI{}, fmt.Errorf("parse command line: --pass-interval must be 0 or more, got %s", cli.PassInterval)
	}
	return cli, nil
}

// buildVersion returns the module version and the VCS revision recorded in
// the binary.
func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	return formatBuildInfo(info)
}

// formatBuildInfo joins the module version, the VCS revision, the VCS time,
// and a "modified" marker when the build tree had local changes.
func formatBuildInfo(info *debug.BuildInfo) string {
	parts := []string{info.Main.Version}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision", "vcs.time":
			parts = append(parts, setting.Value)
		case "vcs.modified":
			if setting.Value == "true" {
				parts = append(parts, "modified")
			}
		}
	}
	return strings.Join(parts, " ")
}

// newLogger returns a logger that writes to w. The format auto selects text
// when isTerminal is true and json otherwise.
func newLogger(w io.Writer, isTerminal bool, format, level string) (*slog.Logger, error) {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("parse log level %q: %w", level, err)
	}
	opts := &slog.HandlerOptions{Level: lvl}
	switch format {
	case formatText:
		return slog.New(slog.NewTextHandler(w, opts)), nil
	case formatJSON:
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	case formatAuto:
		if isTerminal {
			return slog.New(slog.NewTextHandler(w, opts)), nil
		}
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	default:
		return nil, fmt.Errorf("unknown log format %q", format)
	}
}

// run discovers the peers of cli.Host and waits for the steady state.
func run(ctx context.Context, logger *slog.Logger, cli CLI, d discoverer, c checker) error {
	logger.InfoContext(ctx, "starting",
		slog.String(keyHost, cli.Host),
		slog.Int(keyRequiredPasses, cli.RequiredPasses),
	)
	tokensByHost, err := d.Discover(ctx, cli.Host)
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}
	logger.InfoContext(ctx, "discovered peers", slog.Int(keyPeers, len(tokensByHost)))

	cfg := steadystate.Config{
		RequiredPasses: cli.RequiredPasses,
		PassInterval:   cli.PassInterval,
		RetryInterval:  cli.RetryInterval,
	}
	if err := steadystate.Run(ctx, logger, c, tokensByHost, cfg); err != nil {
		return fmt.Errorf("run: %w", err)
	}
	return nil
}
