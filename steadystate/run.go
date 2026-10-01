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

// Package steadystate repeats a peer check until it passes a required number
// of times in a row.
package steadystate

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/retailnext/cassandrasafe/node"
)

const (
	keyError          = "error"
	keyPasses         = "passes"
	keyRequiredPasses = "required_passes"
)

// Checker runs one check of all peers.
type Checker interface {
	Check(ctx context.Context, tokensByHost node.TokensByHost) error
}

// Config controls how Run repeats the check.
type Config struct {
	// RequiredPasses is the number of consecutive passed checks that ends Run.
	RequiredPasses int
	// PassInterval is the pause after a passed check.
	PassInterval time.Duration
	// RetryInterval is the pause after a failed check.
	RetryInterval time.Duration
}

// Run repeats checker.Check until it passes cfg.RequiredPasses times in a row.
// A failed check resets the count. Run returns nil when the required passes
// are reached, and an error when ctx ends first.
func Run(ctx context.Context, logger *slog.Logger, checker Checker, tokensByHost node.TokensByHost, cfg Config) error {
	passes := 0
	for {
		err := checker.Check(ctx, tokensByHost)
		if err != nil {
			passes = 0
			if ctx.Err() != nil {
				return fmt.Errorf("steady state: %w", err)
			}
			logger.InfoContext(ctx, "peer check failed", slog.Any(keyError, err))
			if waitErr := wait(ctx, cfg.RetryInterval); waitErr != nil {
				return fmt.Errorf("steady state: %w", waitErr)
			}
			continue
		}
		passes++
		logger.InfoContext(ctx, "peer check ok",
			slog.Int(keyPasses, passes),
			slog.Int(keyRequiredPasses, cfg.RequiredPasses),
		)
		if passes >= cfg.RequiredPasses {
			logger.InfoContext(ctx, "steady state reached", slog.Int(keyPasses, passes))
			return nil
		}
		if waitErr := wait(ctx, cfg.PassInterval); waitErr != nil {
			return fmt.Errorf("steady state: %w", waitErr)
		}
	}
}

// wait sleeps for d or until ctx ends.
func wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait ended early: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}
