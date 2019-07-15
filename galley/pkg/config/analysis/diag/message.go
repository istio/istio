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

	"istio.io/istio/galley/pkg/config/resource"
)

// Message is a diagnostic message
type Message struct {
	// The error code of the message
	Code Code

	// The level of the message.
	Level Level

	// The Parameters to the message
	Parameters []interface{}

	// Origin of the message
	Origin resource.Origin

	// TODO: Make this localizable
	template string
}

// String implements io.Stringer
func (m *Message) String() string {
	origin := ""
	if m.Origin != nil {
		origin = "(" + m.Origin.FriendlyName() + ")"
	}
	return fmt.Sprintf("[%v%v]%s %s", m.Level, m.Code, origin, fmt.Sprintf(m.template, m.Parameters...))
}

// NewMessage returns a new Message instance.
func NewMessage(l Level, c Code, o resource.Origin, template string, p ...interface{}) Message {
	return Message{
		Level:      l,
		Code:       c,
		Origin:     o,
		template:   template,
		Parameters: p,
	}
}
