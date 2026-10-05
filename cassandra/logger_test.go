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
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestDriverLogger_LevelsAndFields(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	l := driverLogger{logger: logger}

	l.Error("error message", gocql.NewLogFieldString("key", "value"))
	l.Warning("warning message", gocql.NewLogFieldInt("count", 3))
	l.Info("info message", gocql.NewLogFieldBool("flag", true))
	l.Debug("debug message")

	out := buf.String()
	for _, want := range []string{
		"level=DEBUG msg=\"error message\" driver_level=ERROR key=value",
		"level=DEBUG msg=\"warning message\" driver_level=WARN count=3",
		"level=DEBUG msg=\"info message\" driver_level=INFO flag=true",
		"level=DEBUG msg=\"debug message\" driver_level=DEBUG",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not contain %q:\n%s", want, out)
		}
	}
}

func TestDriverLogger_NoOutputAtInfoLevel(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	l := driverLogger{logger: logger}

	l.Error("error message")
	l.Warning("warning message")
	l.Info("info message")
	l.Debug("debug message")

	if out := buf.String(); out != "" {
		t.Errorf("driver logger wrote output at info level:\n%s", out)
	}
}
