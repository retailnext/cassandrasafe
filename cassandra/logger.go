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
	"log/slog"

	"github.com/apache/cassandra-gocql-driver/v2"
)

// driverLogger sends the driver's log messages to a slog.Logger, so the
// driver's warnings appear in the same format as the rest of the output.
type driverLogger struct {
	logger *slog.Logger
}

func (l driverLogger) Error(msg string, fields ...gocql.LogField) {
	l.emit(slog.LevelError, msg, fields)
}

func (l driverLogger) Warning(msg string, fields ...gocql.LogField) {
	l.emit(slog.LevelWarn, msg, fields)
}

func (l driverLogger) Info(msg string, fields ...gocql.LogField) {
	l.emit(slog.LevelInfo, msg, fields)
}

func (l driverLogger) Debug(msg string, fields ...gocql.LogField) {
	l.emit(slog.LevelDebug, msg, fields)
}

func (l driverLogger) emit(level slog.Level, msg string, fields []gocql.LogField) {
	attrs := make([]any, 0, len(fields))
	for _, field := range fields {
		attrs = append(attrs, slog.Attr{Key: field.Name, Value: slog.AnyValue(field.Value.Any())}) //nolint:sloglint // The key names come from the driver.
	}
	//nolint:sloglint // The message text comes from the driver.
	switch level {
	case slog.LevelError:
		l.logger.Error(msg, attrs...)
	case slog.LevelWarn:
		l.logger.Warn(msg, attrs...)
	case slog.LevelInfo:
		l.logger.Info(msg, attrs...)
	case slog.LevelDebug:
		l.logger.Debug(msg, attrs...)
	}
}
