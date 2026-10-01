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

package peercheck_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/retailnext/cassandrasafe/node"
	"github.com/retailnext/cassandrasafe/peercheck"
)

var errDown = errors.New("host down")

// hostScript describes how a fake host answers.
type hostScript struct {
	// failures is the number of attempts that fail before the host answers.
	failures int
	// never makes the host fail every attempt.
	never bool
	// tokens is what the host reports when it answers.
	tokens []string
}

// fakeInspector answers per host according to its script.
type fakeInspector struct {
	mu      sync.Mutex
	scripts map[string]*hostScript
	calls   map[string]int
}

func newFakeInspector(scripts map[string]*hostScript) *fakeInspector {
	return &fakeInspector{scripts: scripts, calls: make(map[string]int)}
}

func (f *fakeInspector) Local(_ context.Context, host string) (node.Local, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[host]++
	script, ok := f.scripts[host]
	if !ok {
		return node.Local{}, errDown
	}
	if script.never || f.calls[host] <= script.failures {
		return node.Local{}, errDown
	}
	return node.Local{Tokens: script.tokens}, nil
}

func (f *fakeInspector) callsFor(host string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[host]
}

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestCheck_AllHostsAnswer(t *testing.T) {
	t.Parallel()
	inspector := newFakeInspector(map[string]*hostScript{
		"192.0.2.10": {failures: 2, tokens: []string{"10", "11"}},
		"192.0.2.20": {tokens: []string{"20"}},
	})
	c := peercheck.New(testLogger(t), inspector, time.Millisecond, time.Millisecond)
	tokens := node.TokensByHost{"192.0.2.10": {"10", "11"}, "192.0.2.20": {"20"}}

	if err := c.Check(t.Context(), tokens); err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if calls := inspector.callsFor("192.0.2.10"); calls != 3 {
		t.Errorf("host 192.0.2.10 was queried %d times, want 3", calls)
	}
}

func TestCheck_EmptyInput(t *testing.T) {
	t.Parallel()
	c := peercheck.New(testLogger(t), newFakeInspector(nil), time.Millisecond, time.Millisecond)
	if err := c.Check(t.Context(), node.TokensByHost{}); err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
}

func TestCheck_StopsWhenTokensAreCoveredByOtherHosts(t *testing.T) {
	t.Parallel()
	// 192.0.2.30 is a stale system.peers row. It never answers, but it owns
	// no tokens, so the check completes without it.
	inspector := newFakeInspector(map[string]*hostScript{
		"192.0.2.10": {tokens: []string{"10"}},
		"192.0.2.30": {never: true},
	})
	c := peercheck.New(testLogger(t), inspector, time.Millisecond, time.Millisecond)
	tokens := node.TokensByHost{"192.0.2.10": {"10"}, "192.0.2.30": nil}

	done := make(chan error, 1)
	go func() { done <- c.Check(t.Context(), tokens) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Check returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Check did not complete once all tokens were seen")
	}
}

func TestCheck_ReportedTokensDoNotCoverPeersRow(t *testing.T) {
	t.Parallel()
	// Every host answers, but 192.0.2.20 reports no tokens, so token 20 stays
	// outstanding and the check fails with no unanswered hosts.
	inspector := newFakeInspector(map[string]*hostScript{
		"192.0.2.10": {tokens: []string{"10"}},
		"192.0.2.20": {tokens: nil},
	})
	c := peercheck.New(testLogger(t), inspector, time.Millisecond, time.Millisecond)
	tokens := node.TokensByHost{"192.0.2.10": {"10"}, "192.0.2.20": {"20"}}

	err := c.Check(t.Context(), tokens)
	var unresponsive *peercheck.UnresponsiveHostsError
	if !errors.As(err, &unresponsive) {
		t.Fatalf("error %v is not an UnresponsiveHostsError", err)
	}
	if len(unresponsive.Hosts) != 0 {
		t.Errorf("unanswered hosts %v, want none", unresponsive.Hosts)
	}
	if unresponsive.OutstandingTokens != 1 {
		t.Errorf("outstanding tokens %d, want 1", unresponsive.OutstandingTokens)
	}
	if errors.Is(err, context.Canceled) {
		t.Errorf("error %v wraps context.Canceled, but the context did not end", err)
	}
}

func TestCheck_CancelReportsUnansweredHosts(t *testing.T) {
	t.Parallel()
	inspector := newFakeInspector(map[string]*hostScript{
		"192.0.2.10": {tokens: []string{"10"}},
		"192.0.2.20": {never: true},
		"192.0.2.30": {never: true},
	})
	c := peercheck.New(testLogger(t), inspector, time.Millisecond, time.Millisecond)
	tokens := node.TokensByHost{"192.0.2.10": {"10"}, "192.0.2.20": {"20"}, "192.0.2.30": {"30"}}
	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan error, 1)
	go func() { done <- c.Check(ctx, tokens) }()
	time.Sleep(20 * time.Millisecond)
	cancel()

	var err error
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Check did not return after cancel")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error %v does not wrap context.Canceled", err)
	}
	var unresponsive *peercheck.UnresponsiveHostsError
	if !errors.As(err, &unresponsive) {
		t.Fatalf("error %v is not an UnresponsiveHostsError", err)
	}
	want := []string{"192.0.2.20", "192.0.2.30"}
	if len(unresponsive.Hosts) != len(want) || unresponsive.Hosts[0] != want[0] || unresponsive.Hosts[1] != want[1] {
		t.Errorf("unanswered hosts %v, want %v", unresponsive.Hosts, want)
	}
	if unresponsive.OutstandingTokens != 2 {
		t.Errorf("outstanding tokens %d, want 2", unresponsive.OutstandingTokens)
	}
	if msg := unresponsive.Error(); msg != "2 hosts did not answer and 2 tokens are outstanding" {
		t.Errorf("unexpected message %q", msg)
	}
}

func TestCheck_StatusIsLoggedWhileWaiting(t *testing.T) {
	t.Parallel()
	inspector := newFakeInspector(map[string]*hostScript{
		"192.0.2.10": {failures: 50, tokens: []string{"10"}},
	})
	c := peercheck.New(testLogger(t), inspector, time.Millisecond, time.Millisecond)

	if err := c.Check(t.Context(), node.TokensByHost{"192.0.2.10": {"10"}}); err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
}
