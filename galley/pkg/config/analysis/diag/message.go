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

// MessageType is a type of diagnostic message
type MessageType struct {
	// The level of the message.
	level Level

	// The error code of the message
	code string

	// TODO: Make this localizable
	template string
}

// Level returns the level of the MessageType
func (m *MessageType) Level() Level { return m.level }

// Code returns the code of the MessageType
func (m *MessageType) Code() string { return m.code }

// Template returns the message template used by the MessageType
func (m *MessageType) Template() string { return m.template }

// Message is a specific diagnostic message
type Message struct {
	Type *MessageType

	// The Parameters to the message
	Parameters []interface{}

	// Origin of the message
	Origin resource.Origin
}

// Unstructured returns this message as a JSON-style unstructured map
func (m *Message) Unstructured(includeOrigin bool) map[string]interface{} {
	result := make(map[string]interface{})

	result["code"] = m.Type.Code()
	result["level"] = m.Type.Level().String()
	if includeOrigin && m.Origin != nil {
		result["origin"] = m.Origin.FriendlyName()
	}
	result["message"] = fmt.Sprintf(m.Type.Template(), m.Parameters...)

	return result
}

// String implements io.Stringer
func (m *Message) String() string {
	origin := ""
	if m.Origin != nil {
		origin = "(" + m.Origin.FriendlyName() + ")"
	}
	return fmt.Sprintf(
		"%v [%v]%s %s", m.Type.Level(), m.Type.Code(), origin, fmt.Sprintf(m.Type.Template(), m.Parameters...))
}

// NewMessageType returns a new MessageType instance.
func NewMessageType(level Level, code, template string) *MessageType {
	return &MessageType{
		level:    level,
		code:     code,
		template: template,
	}
}

// NewMessage returns a new Message instance from an existing type.
func NewMessage(mt *MessageType, o resource.Origin, p ...interface{}) Message {
	return Message{
		Type:       mt,
		Origin:     o,
		Parameters: p,
	}
}
