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

package cassandra

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestLocal_ConnectionRefused(t *testing.T) {
	t.Parallel()
	client := New(testLogger(t), Options{ConnectTimeout: time.Second, QueryTimeout: time.Second})

	_, err := client.Local(t.Context(), "127.0.0.1:1")
	if err == nil {
		t.Fatal("Local returned nil, want a connection error")
	}
}

func TestLocalAndPeers_ConnectionRefused(t *testing.T) {
	t.Parallel()
	client := New(testLogger(t), Options{})

	_, _, err := client.LocalAndPeers(t.Context(), "127.0.0.1:1")
	if err == nil {
		t.Fatal("LocalAndPeers returned nil, want a connection error")
	}
}

func TestLocal_CancelledContext(t *testing.T) {
	t.Parallel()
	client := New(testLogger(t), Options{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := client.Local(ctx, "127.0.0.1:1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v does not wrap context.Canceled", err)
	}
}

func TestLocalAndPeers_CancelledContext(t *testing.T) {
	t.Parallel()
	client := New(testLogger(t), Options{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := client.LocalAndPeers(ctx, "127.0.0.1:1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v does not wrap context.Canceled", err)
	}
}
