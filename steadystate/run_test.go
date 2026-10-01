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

package steadystate_test

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/retailnext/cassandrasafe/node"
	"github.com/retailnext/cassandrasafe/steadystate"
)

var errCheck = errors.New("check failed")

// scriptedChecker returns the results in order and then repeats the last one.
type scriptedChecker struct {
	results []error
	calls   atomic.Int32
}

func (s *scriptedChecker) Check(_ context.Context, _ node.TokensByHost) error {
	call := int(s.calls.Add(1)) - 1
	if call >= len(s.results) {
		call = len(s.results) - 1
	}
	return s.results[call]
}

// blockingChecker waits for ctx to end and then returns errCheck.
type blockingChecker struct{}

func (blockingChecker) Check(ctx context.Context, _ node.TokensByHost) error {
	<-ctx.Done()
	return errCheck
}

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(t.Output(), nil))
}

func TestRun_ResetsCountAfterFailure(t *testing.T) {
	t.Parallel()
	checker := &scriptedChecker{results: []error{nil, nil, errCheck, nil, nil, nil}}
	cfg := steadystate.Config{RequiredPasses: 3, PassInterval: time.Millisecond, RetryInterval: time.Millisecond}

	err := steadystate.Run(t.Context(), testLogger(t), checker, node.TokensByHost{}, cfg)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if calls := checker.calls.Load(); calls != 6 {
		t.Errorf("checker was called %d times, want 6", calls)
	}
}

func TestRun_SinglePass(t *testing.T) {
	t.Parallel()
	checker := &scriptedChecker{results: []error{nil}}
	cfg := steadystate.Config{RequiredPasses: 1, PassInterval: time.Hour, RetryInterval: time.Hour}

	if err := steadystate.Run(t.Context(), testLogger(t), checker, node.TokensByHost{}, cfg); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if calls := checker.calls.Load(); calls != 1 {
		t.Errorf("checker was called %d times, want 1", calls)
	}
}

func TestRun_ReturnsCheckErrorOnCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cfg := steadystate.Config{RequiredPasses: 2, PassInterval: time.Hour, RetryInterval: time.Hour}
	done := make(chan error, 1)
	go func() {
		done <- steadystate.Run(ctx, testLogger(t), blockingChecker{}, node.TokensByHost{}, cfg)
	}()
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, errCheck) {
			t.Errorf("error %v does not wrap the check error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestRun_CancelDuringPassInterval(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	checker := &scriptedChecker{results: []error{nil}}
	cfg := steadystate.Config{RequiredPasses: 2, PassInterval: time.Hour, RetryInterval: time.Hour}
	done := make(chan error, 1)
	go func() {
		done <- steadystate.Run(ctx, testLogger(t), checker, node.TokensByHost{}, cfg)
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("error %v does not wrap context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestRun_CancelDuringRetryInterval(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	checker := &scriptedChecker{results: []error{errCheck}}
	cfg := steadystate.Config{RequiredPasses: 2, PassInterval: time.Hour, RetryInterval: time.Hour}
	done := make(chan error, 1)
	go func() {
		done <- steadystate.Run(ctx, testLogger(t), checker, node.TokensByHost{}, cfg)
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("error %v does not wrap context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
