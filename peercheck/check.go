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

// Package peercheck waits until the peers of a Cassandra node answer CQL
// queries and account for every token that the node knows about.
package peercheck

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/retailnext/cassandrasafe/node"
)

const (
	keyAttempt           = "attempt"
	keyError             = "error"
	keyHost              = "host"
	keyOutstandingHosts  = "outstanding_hosts"
	keyOutstandingTokens = "outstanding_tokens"
	keyTokens            = "tokens"
)

// Inspector reads the system.local row of one node.
type Inspector interface {
	Local(ctx context.Context, host string) (node.Local, error)
}

// Checker runs one check of all peers at a time.
type Checker struct {
	logger         *slog.Logger
	inspector      Inspector
	retryInterval  time.Duration
	statusInterval time.Duration
}

// New returns a Checker. It waits retryInterval between failed attempts on one
// host and logs the outstanding hosts every statusInterval.
func New(logger *slog.Logger, inspector Inspector, retryInterval, statusInterval time.Duration) *Checker {
	return &Checker{
		logger:         logger,
		inspector:      inspector,
		retryInterval:  retryInterval,
		statusInterval: statusInterval,
	}
}

// Check queries every host in tokensByHost at the same time. Each host is
// queried again until it answers. A host that answers accounts for the tokens
// that it reports. The check ends when every token in tokensByHost is
// accounted for, when every host has answered, or when ctx ends.
//
// Check returns nil when every token is accounted for. Otherwise it returns an
// *UnresponsiveHostsError. When ctx ended, the error also wraps ctx.Err().
// No goroutine that Check starts outlives its return.
func (c *Checker) Check(ctx context.Context, tokensByHost node.TokensByHost) error {
	runCtx, stop := context.WithCancel(ctx)
	defer stop()

	progress := newProgress(tokensByHost)
	var wg sync.WaitGroup
	for host := range tokensByHost {
		wg.Go(func() {
			c.checkHost(runCtx, progress, host, stop)
		})
	}

	statusDone := make(chan struct{})
	go func() {
		defer close(statusDone)
		c.reportStatus(runCtx, progress)
	}()

	wg.Wait()
	stop()
	<-statusDone
	return progress.result(ctx.Err())
}

// checkHost queries host until it answers or ctx ends. It calls stop when the
// answer accounts for the last outstanding token.
func (c *Checker) checkHost(ctx context.Context, progress *progress, host string, stop context.CancelFunc) {
	logger := c.logger.With(slog.String(keyHost, host))
	for attempt := 1; ; attempt++ {
		logger.DebugContext(ctx, "host attempting", slog.Int(keyAttempt, attempt))
		local, err := c.inspector.Local(ctx, host)
		if err == nil {
			logger.DebugContext(ctx, "got node info", slog.Int(keyTokens, len(local.Tokens)))
			if progress.answered(host, local.Tokens) {
				stop()
			}
			return
		}
		logger.DebugContext(ctx, "host attempt failed", slog.Int(keyAttempt, attempt), slog.Any(keyError, err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(c.retryInterval):
		}
	}
}

// reportStatus logs the outstanding hosts and tokens until ctx ends.
func (c *Checker) reportStatus(ctx context.Context, progress *progress) {
	ticker := time.NewTicker(c.statusInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hosts, tokens := progress.outstanding()
			c.logger.InfoContext(ctx, "waiting for hosts",
				slog.Any(keyOutstandingHosts, hosts),
				slog.Int(keyOutstandingTokens, tokens),
			)
		}
	}
}
