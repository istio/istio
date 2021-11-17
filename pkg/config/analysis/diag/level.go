// Copyright Istio Authors
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
	"strings"
)

// Level is the severity level of a message.
type Level struct {
	sortOrder int
	name      string
}

func (l Level) String() string {
	return l.name
}

func (l Level) IsWorseThanOrEqualTo(target Level) bool {
	return l.sortOrder <= target.sortOrder
}

var (
	// Info level is for informational messages
	Info = Level{2, "Info"}

	// Warning level is for warning messages
	Warning = Level{1, "Warning"}

	// Error level is for error messages
	Error = Level{0, "Error"}
)

// GetAllLevels returns an arbitrarily ordered slice of all Levels defined.
func GetAllLevels() []Level {
	return []Level{Info, Warning, Error}
}

// GetAllLevelStrings returns a list of strings representing the names of all Levels defined. The order is arbitrary but
// should be the same as GetAllLevels.
func GetAllLevelStrings() []string {
	levels := GetAllLevels()
	var s []string
	for _, l := range levels {
		s = append(s, l.name)
	}
	return s
}

// GetUppercaseStringToLevelMap returns a mapping of uppercase strings to Level structs. This function is intended to be
// used to convert user input to structs.
func GetUppercaseStringToLevelMap() map[string]Level {
	m := make(map[string]Level)
	for _, l := range GetAllLevels() {
		m[strings.ToUpper(l.name)] = l
	}
	return m
}
