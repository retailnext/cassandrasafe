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

package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/retailnext/cassandrasafe/node"
)

var (
	errDiscover = errors.New("discover failed")
	errCheck    = errors.New("check failed")
)

type fakeDiscoverer struct {
	tokens node.TokensByHost
	err    error
}

func (f fakeDiscoverer) Discover(_ context.Context, _ string) (node.TokensByHost, error) {
	return f.tokens, f.err
}

type fakeChecker struct {
	err error
}

func (f fakeChecker) Check(_ context.Context, _ node.TokensByHost) error {
	return f.err
}

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(t.Output(), nil))
}

func testCLI() CLI {
	return CLI{
		Host:           "192.0.2.1",
		RequiredPasses: 2,
		PassInterval:   time.Millisecond,
		RetryInterval:  time.Millisecond,
		StatusInterval: time.Millisecond,
	}
}

func TestParseCLI_Defaults(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	cli, err := parseCLI(nil, &stdout, func(int) {})
	if err != nil {
		t.Fatalf("parseCLI returned error: %v", err)
	}
	want := CLI{
		Host:           "127.0.0.1",
		RequiredPasses: 6,
		PassInterval:   5 * time.Second,
		RetryInterval:  time.Second,
		StatusInterval: time.Second,
		LogFormat:      formatAuto,
		LogLevel:       "info",
	}
	if cli != want {
		t.Errorf("defaults %+v, want %+v", cli, want)
	}
}

func TestParseCLI_Overrides(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	args := []string{
		"--host=192.0.2.5:9043", "--required-passes=2", "--pass-interval=1s",
		"--retry-interval=500ms", "--status-interval=2s", "--log-format=json", "--log-level=debug",
	}
	cli, err := parseCLI(args, &stdout, func(int) {})
	if err != nil {
		t.Fatalf("parseCLI returned error: %v", err)
	}
	if cli.Host != "192.0.2.5:9043" || cli.RequiredPasses != 2 || cli.PassInterval != time.Second ||
		cli.RetryInterval != 500*time.Millisecond || cli.StatusInterval != 2*time.Second ||
		cli.LogFormat != formatJSON || cli.LogLevel != "debug" {
		t.Errorf("unexpected parse result %+v", cli)
	}
}

func TestParseCLI_Rejects(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"unknown flag":           {"--bogus"},
		"bad enum":               {"--log-format=xml"},
		"zero required passes":   {"--required-passes=0"},
		"zero retry interval":    {"--retry-interval=0"},
		"zero status interval":   {"--status-interval=0"},
		"negative pass interval": {"--pass-interval=-1s"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var stdout bytes.Buffer
			if _, err := parseCLI(args, &stdout, func(int) {}); err == nil {
				t.Errorf("parseCLI(%v) returned nil, want error", args)
			}
		})
	}
}

func TestParseCLI_VersionAndHelp(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"--version", "--help"} {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()
			var stdout bytes.Buffer
			exitCode := -1
			_, err := parseCLI([]string{flag}, &stdout, func(code int) { exitCode = code })
			if err != nil {
				t.Fatalf("parseCLI returned error: %v", err)
			}
			if exitCode != 0 {
				t.Errorf("exit code %d, want 0", exitCode)
			}
			if stdout.Len() == 0 {
				t.Errorf("nothing was written to stdout")
			}
		})
	}
}

func TestBuildVersion(t *testing.T) {
	t.Parallel()
	if buildVersion() == "" {
		t.Error("buildVersion returned an empty string")
	}
}

func TestFormatBuildInfo(t *testing.T) {
	t.Parallel()
	info := &debug.BuildInfo{
		Main: debug.Module{Version: "v1.2.3"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abc123"},
			{Key: "vcs.time", Value: "2026-01-02T03:04:05Z"},
			{Key: "vcs.modified", Value: "true"},
			{Key: "CGO_ENABLED", Value: "0"},
		},
	}
	if got, want := formatBuildInfo(info), "v1.2.3 abc123 2026-01-02T03:04:05Z modified"; got != want {
		t.Errorf("formatBuildInfo = %q, want %q", got, want)
	}
	info.Settings[2].Value = "false"
	if got, want := formatBuildInfo(info), "v1.2.3 abc123 2026-01-02T03:04:05Z"; got != want {
		t.Errorf("formatBuildInfo = %q, want %q", got, want)
	}
}

// stderrFile returns a regular file that stands in for stderr. A regular file
// is not a terminal, so execute selects the json log format.
func stderrFile(t *testing.T) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func readFile(t *testing.T, f *os.File) string {
	t.Helper()
	content, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("read %s: %v", f.Name(), err)
	}
	return string(content)
}

func TestIsTerminal_RegularFile(t *testing.T) {
	t.Parallel()
	if isTerminal(stderrFile(t)) {
		t.Error("a regular file was reported as a terminal")
	}
}

func TestExecute_UsageError(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	stderr := stderrFile(t)
	code := execute(t.Context(), []string{"--required-passes=0"}, &stdout, stderr, func(int) {})
	if code != exitUsage {
		t.Errorf("exit code %d, want %d", code, exitUsage)
	}
	if !strings.Contains(readFile(t, stderr), "--required-passes") {
		t.Errorf("stderr does not name the bad flag: %q", readFile(t, stderr))
	}
}

func TestExecute_ExitsWithErrorWhenCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	var stdout bytes.Buffer
	stderr := stderrFile(t)
	args := []string{"--host=127.0.0.1:1", "--retry-interval=10ms", "--status-interval=10ms"}
	done := make(chan int, 1)
	go func() { done <- execute(ctx, args, &stdout, stderr, func(int) {}) }()
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case code := <-done:
		if code != exitError {
			t.Errorf("exit code %d, want %d", code, exitError)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("execute did not return after cancel")
	}
	out := readFile(t, stderr)
	if !strings.HasPrefix(out, "{") {
		t.Errorf("stderr is not json: %q", out)
	}
	if !strings.Contains(out, "exiting with error") {
		t.Errorf("stderr does not report the error: %q", out)
	}
}

func TestNewLogger_FormatSelection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		format     string
		isTerminal bool
		wantJSON   bool
	}{
		{name: "text", format: formatText, isTerminal: false, wantJSON: false},
		{name: "json", format: formatJSON, isTerminal: true, wantJSON: true},
		{name: "auto on terminal", format: formatAuto, isTerminal: true, wantJSON: false},
		{name: "auto without terminal", format: formatAuto, isTerminal: false, wantJSON: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			logger, err := newLogger(&buf, tc.isTerminal, tc.format, "info")
			if err != nil {
				t.Fatalf("newLogger returned error: %v", err)
			}
			logger.InfoContext(t.Context(), "probe")
			gotJSON := strings.HasPrefix(buf.String(), "{")
			if gotJSON != tc.wantJSON {
				t.Errorf("json output %v, want %v: %q", gotJSON, tc.wantJSON, buf.String())
			}
		})
	}
}

func TestNewLogger_Level(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger, err := newLogger(&buf, false, formatText, "warn")
	if err != nil {
		t.Fatalf("newLogger returned error: %v", err)
	}
	logger.InfoContext(t.Context(), "hidden")
	logger.WarnContext(t.Context(), "shown")
	if strings.Contains(buf.String(), "hidden") || !strings.Contains(buf.String(), "shown") {
		t.Errorf("unexpected output %q", buf.String())
	}
}

func TestNewLogger_Rejects(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if _, err := newLogger(&buf, false, formatText, "loud"); err == nil {
		t.Error("newLogger accepted an unknown level")
	}
	if _, err := newLogger(&buf, false, "xml", "info"); err == nil {
		t.Error("newLogger accepted an unknown format")
	}
}

func TestRun_ReachesSteadyState(t *testing.T) {
	t.Parallel()
	d := fakeDiscoverer{tokens: node.TokensByHost{"192.0.2.10": {"10"}}}
	if err := run(t.Context(), testLogger(t), testCLI(), d, fakeChecker{}); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
}

func TestRun_DiscoverError(t *testing.T) {
	t.Parallel()
	err := run(t.Context(), testLogger(t), testCLI(), fakeDiscoverer{err: errDiscover}, fakeChecker{})
	if !errors.Is(err, errDiscover) {
		t.Fatalf("error %v does not wrap the discover error", err)
	}
}

func TestRun_CheckErrorAfterCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := run(ctx, testLogger(t), testCLI(), fakeDiscoverer{}, fakeChecker{err: errCheck})
	if !errors.Is(err, errCheck) {
		t.Fatalf("error %v does not wrap the check error", err)
	}
}
