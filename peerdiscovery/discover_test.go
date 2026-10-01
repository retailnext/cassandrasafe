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

package peerdiscovery_test

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/retailnext/cassandrasafe/node"
	"github.com/retailnext/cassandrasafe/peerdiscovery"
)

var errUnavailable = errors.New("node unavailable")

// fakeInspector fails the first failures calls and then returns local and peers.
type fakeInspector struct {
	failures int32
	calls    atomic.Int32
	local    node.Local
	peers    []node.Peer
}

func (f *fakeInspector) LocalAndPeers(_ context.Context, _ string) (node.Local, []node.Peer, error) {
	call := f.calls.Add(1)
	if call <= f.failures {
		return node.Local{}, nil, errUnavailable
	}
	return f.local, f.peers, nil
}

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestDiscover_FiltersOtherDatacenters(t *testing.T) {
	t.Parallel()
	inspector := &fakeInspector{
		local: node.Local{DataCenter: "dc1", Tokens: []string{"1"}},
		peers: []node.Peer{
			{Address: "192.0.2.10", DataCenter: "dc1", Tokens: []string{"10", "11"}},
			{Address: "192.0.2.20", DataCenter: "dc2", Tokens: []string{"20"}},
			{Address: "192.0.2.30", DataCenter: "dc1", Tokens: nil},
		},
	}
	d := peerdiscovery.New(testLogger(t), inspector, time.Millisecond)

	got, err := d.Discover(t.Context(), "192.0.2.1")
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d peers, want 2: %v", len(got), got)
	}
	if tokens := got["192.0.2.10"]; len(tokens) != 2 {
		t.Errorf("peer 192.0.2.10 has tokens %v, want 2 tokens", tokens)
	}
	if _, ok := got["192.0.2.30"]; !ok {
		t.Errorf("peer 192.0.2.30 without tokens is missing")
	}
	if _, ok := got["192.0.2.20"]; ok {
		t.Errorf("peer 192.0.2.20 in another datacenter was kept")
	}
}

func TestDiscover_RetriesUntilSuccess(t *testing.T) {
	t.Parallel()
	inspector := &fakeInspector{
		failures: 3,
		local:    node.Local{DataCenter: "dc1"},
		peers:    []node.Peer{{Address: "192.0.2.10", DataCenter: "dc1", Tokens: []string{"10"}}},
	}
	d := peerdiscovery.New(testLogger(t), inspector, time.Millisecond)

	got, err := d.Discover(t.Context(), "192.0.2.1")
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	if calls := inspector.calls.Load(); calls != 4 {
		t.Errorf("inspector was called %d times, want 4", calls)
	}
	if len(got) != 1 {
		t.Errorf("got %d peers, want 1", len(got))
	}
}

func TestDiscover_ReturnsBothErrorsOnCancel(t *testing.T) {
	t.Parallel()
	inspector := &fakeInspector{failures: 1 << 30}
	d := peerdiscovery.New(testLogger(t), inspector, time.Hour)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := d.Discover(ctx, "192.0.2.1")
	if err == nil {
		t.Fatal("Discover returned nil, want error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error %v does not wrap context.Canceled", err)
	}
	if !errors.Is(err, errUnavailable) {
		t.Errorf("error %v does not wrap the last inspector error", err)
	}
}

func TestDiscover_CancelDuringRetryWait(t *testing.T) {
	t.Parallel()
	inspector := &fakeInspector{failures: 1 << 30}
	d := peerdiscovery.New(testLogger(t), inspector, time.Hour)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := d.Discover(ctx, "192.0.2.1")
		done <- err
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("error %v does not wrap context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Discover did not return after cancel")
	}
}
