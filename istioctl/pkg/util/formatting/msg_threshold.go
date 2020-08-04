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

package formatting

import (
	"errors"
	"strings"

	"istio.io/istio/galley/pkg/config/analysis/diag"
)

// MessageThreshold is a wrapper around Level to be used as a cobra command line argument.
// It should satisfy the pflag.Value interface.
type MessageThreshold struct {
	diag.Level
}

// String is a function declared in the pflag.Value interface
func (m *MessageThreshold) String() string {
	return m.Level.String()
}

// Type is a function declared in the pflag.Value interface
func (m *MessageThreshold) Type() string {
	return "Level"
}

// Set is a function declared in the pflag.Value interface
func (m *MessageThreshold) Set(s string) error {
	levelMap := diag.GetUppercaseStringToLevelMap()
	level, ok := levelMap[strings.ToUpper(s)]
	if !ok {
		return errors.New("invalid level option")
	}
	m.Level = level
	return nil
}
