// Copyright 2019 Istio Authors
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

package diag

import (
	"fmt"
	"strings"
)

// Valid values for Level input via CLI
var ValidStringLevels = []string{
	"INFO",
	"WARN",
	"WARNING",
	"ERR",
	"ERROR",
}

// Level is the severity level of a message.
type Level struct {
	sortOrder int
	name      string
}

// String satisfies interface pflag.Value
func (l *Level) String() string {
	return l.name
}

// Type satisfies interface pflag.Value
func (l *Level) Type() string {
	return "Level"
}

// Set satisfies interface pflag.Value
func (l *Level) Set(s string) error {
	switch strings.ToUpper(s) {
	case "INFO":
		l.sortOrder = Info.sortOrder
		l.name = Info.name
	case "WARN", "WARNING":
		l.sortOrder = Warning.sortOrder
		l.name = Warning.name
	case "ERR", "ERROR":
		l.sortOrder = Error.sortOrder
		l.name = Error.name
	default:
		return fmt.Errorf("%q not a valid option, please choose from: %v", s, ValidStringLevels)
	}

	return nil
}

func (l *Level) IsWorseThanOrEqualTo(target Level) bool {
	return l.sortOrder <= target.sortOrder
}

var (
	// Info level is for informational messages
	Info = Level{2, "Info"}

	// Warning level is for warning messages
	Warning = Level{1, "Warn"}

	// Error level is for error messages
	Error = Level{0, "Error"}
)
