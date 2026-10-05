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

// Package cassandra reads the system tables of one Cassandra node through the
// CQL native protocol. It is the only package that imports the driver.
package cassandra

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/retailnext/cassandrasafe/node"
)

const (
	keyDriverLevel = "driver_level"
	keyHost        = "host"

	queryLocal = `SELECT bootstrapped, cluster_name, data_center, host_id, partitioner, rack, tokens FROM system.local`
	queryPeers = `SELECT peer, data_center, host_id, preferred_ip, rack, release_version, rpc_address, schema_version, tokens FROM system.peers`
)

// Options sets the driver timeouts. A zero value keeps the driver default.
type Options struct {
	// ConnectTimeout bounds the connection to one node.
	ConnectTimeout time.Duration
	// QueryTimeout bounds one query.
	QueryTimeout time.Duration
}

// Client reads the system tables of one node for each call.
type Client struct {
	logger  *slog.Logger
	options Options
}

// New returns a Client that logs through logger.
func New(logger *slog.Logger, options Options) *Client {
	return &Client{logger: logger, options: options}
}

// Local returns the system.local row of host.
func (c *Client) Local(ctx context.Context, host string) (node.Local, error) {
	var local node.Local
	err := c.withSession(ctx, host, func(session *gocql.Session) error {
		return scanLocal(ctx, session, &local)
	})
	if err != nil {
		return node.Local{}, fmt.Errorf("read system.local from %s: %w", host, err)
	}
	return local, nil
}

// LocalAndPeers returns the system.local row and all system.peers rows of
// host.
func (c *Client) LocalAndPeers(ctx context.Context, host string) (node.Local, []node.Peer, error) {
	var local node.Local
	var peers []node.Peer
	err := c.withSession(ctx, host, func(session *gocql.Session) error {
		if err := scanLocal(ctx, session, &local); err != nil {
			return err
		}
		return scanPeers(ctx, session, &peers)
	})
	if err != nil {
		return node.Local{}, nil, fmt.Errorf("read system tables from %s: %w", host, err)
	}
	return local, peers, nil
}

// withSession opens a session to host, runs f, and closes the session.
// The session uses one connection and does not look up other hosts, so a
// single node is enough to answer.
func (c *Client) withSession(ctx context.Context, host string, f func(*gocql.Session) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("before connect: %w", err)
	}
	cluster := gocql.NewCluster(host)
	cluster.NumConns = 1
	cluster.DisableInitialHostLookup = true
	cluster.Consistency = gocql.LocalOne
	cluster.Logger = driverLogger{logger: c.logger.With(slog.String(keyHost, host))}
	if c.options.ConnectTimeout > 0 {
		cluster.ConnectTimeout = c.options.ConnectTimeout
	}
	if c.options.QueryTimeout > 0 {
		cluster.Timeout = c.options.QueryTimeout
	}
	session, err := cluster.CreateSession()
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	defer session.Close()
	return f(session)
}

func scanLocal(ctx context.Context, session *gocql.Session, local *node.Local) error {
	err := session.Query(queryLocal).ScanContext(ctx,
		&local.BootstrapState,
		&local.ClusterName,
		&local.DataCenter,
		&local.HostID,
		&local.Partitioner,
		&local.Rack,
		&local.Tokens,
	)
	if err != nil {
		return fmt.Errorf("query system.local: %w", err)
	}
	return nil
}

func scanPeers(ctx context.Context, session *gocql.Session, peers *[]node.Peer) error {
	iter := session.Query(queryPeers).IterContext(ctx)
	var result []node.Peer
	for {
		var peer node.Peer
		ok := iter.Scan(
			&peer.Address,
			&peer.DataCenter,
			&peer.HostID,
			&peer.PreferredIP,
			&peer.Rack,
			&peer.ReleaseVersion,
			&peer.RPCAddress,
			&peer.SchemaVersion,
			&peer.Tokens,
		)
		if !ok {
			break
		}
		result = append(result, peer)
	}
	if err := iter.Close(); err != nil {
		return fmt.Errorf("query system.peers: %w", err)
	}
	*peers = result
	return nil
}
