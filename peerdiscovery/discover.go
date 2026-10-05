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

// Package peerdiscovery finds the peers of a Cassandra node that share its
// datacenter, and the tokens that each of those peers owns.
package peerdiscovery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/retailnext/cassandrasafe/node"
)

const (
	keyAttempt         = "attempt"
	keyError           = "error"
	keyHost            = "host"
	keyIgnoredPeers    = "ignored_peers"
	keyLocal           = "local"
	keyLocalDatacenter = "local_datacenter"
	keyPeer            = "peer"
	keyPeerDatacenter  = "peer_datacenter"
	keyPeers           = "peers"
)

// Inspector reads the system tables of one node.
type Inspector interface {
	LocalAndPeers(ctx context.Context, host string) (node.Local, []node.Peer, error)
}

// Discoverer finds the same-datacenter peers of a node.
type Discoverer struct {
	logger        *slog.Logger
	inspector     Inspector
	retryInterval time.Duration
}

// New returns a Discoverer that waits retryInterval between failed attempts.
func New(logger *slog.Logger, inspector Inspector, retryInterval time.Duration) *Discoverer {
	return &Discoverer{logger: logger, inspector: inspector, retryInterval: retryInterval}
}

// Discover queries host until it answers. It then returns the tokens of each
// peer that is in the same datacenter as host. It returns an error only when
// ctx ends first.
func (d *Discoverer) Discover(ctx context.Context, host string) (node.TokensByHost, error) {
	logger := d.logger.With(slog.String(keyHost, host))
	var lastErr error
	for attempt := 1; ; attempt++ {
		local, peers, err := d.inspector.LocalAndPeers(ctx, host)
		if err == nil {
			return selectPeers(ctx, logger, local, peers), nil
		}
		lastErr = err
		logger.InfoContext(ctx, "waiting for host", slog.Int(keyAttempt, attempt), slog.Any(keyError, err))
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("discover peers of %s: %w", host, errors.Join(ctx.Err(), lastErr))
		case <-time.After(d.retryInterval):
		}
	}
}

// selectPeers keeps the peers that share the datacenter of local.
func selectPeers(ctx context.Context, logger *slog.Logger, local node.Local, peers []node.Peer) node.TokensByHost {
	result := make(node.TokensByHost, len(peers))
	ignored := 0
	for _, peer := range peers {
		if peer.DataCenter != local.DataCenter {
			ignored++
			logger.DebugContext(ctx, "ignoring peer in other datacenter",
				slog.String(keyLocalDatacenter, local.DataCenter),
				slog.String(keyPeerDatacenter, peer.DataCenter),
				slog.String(keyPeer, peer.Address),
			)
			continue
		}
		result[peer.Address] = peer.Tokens
	}
	if ignored > 0 {
		logger.InfoContext(ctx, "ignored peers in other datacenters",
			slog.String(keyLocalDatacenter, local.DataCenter),
			slog.Int(keyIgnoredPeers, ignored),
		)
	}
	logger.DebugContext(ctx, "got host info", slog.Any(keyLocal, local), slog.Any(keyPeers, peers))
	return result
}
