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
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/retailnext/cassandrasafe/cassandra"
	"github.com/retailnext/cassandrasafe/peercheck"
	"github.com/retailnext/cassandrasafe/peerdiscovery"
)

// TestIntegration_SingleNode runs the real driver against one Cassandra node.
// Set CASSANDRASAFE_TEST_HOST to the host, or host:port, of that node.
func TestIntegration_SingleNode(t *testing.T) {
	t.Parallel()
	host := os.Getenv("CASSANDRASAFE_TEST_HOST")
	if host == "" {
		t.Skip("CASSANDRASAFE_TEST_HOST is not set")
	}
	logger := slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelDebug}))
	client := cassandra.New(logger, cassandra.Options{})

	local, peers, err := client.LocalAndPeers(t.Context(), host)
	if err != nil {
		t.Fatalf("LocalAndPeers returned error: %v", err)
	}
	if local.ClusterName == "" || local.DataCenter == "" || len(local.Tokens) == 0 {
		t.Errorf("incomplete system.local row: %+v", local)
	}
	t.Logf("node %s in %s has %d peers", local.HostID, local.DataCenter, len(peers))

	again, err := client.Local(t.Context(), host)
	if err != nil {
		t.Fatalf("Local returned error: %v", err)
	}
	if again.HostID != local.HostID {
		t.Errorf("Local returned host %s, LocalAndPeers returned host %s", again.HostID, local.HostID)
	}

	cli := CLI{
		Host:           host,
		RequiredPasses: 2,
		PassInterval:   10 * time.Millisecond,
		RetryInterval:  100 * time.Millisecond,
		StatusInterval: 100 * time.Millisecond,
	}
	d := peerdiscovery.New(logger, client, cli.RetryInterval)
	c := peercheck.New(logger, client, cli.RetryInterval, cli.StatusInterval)
	if err := run(t.Context(), logger, cli, d, c); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
}
