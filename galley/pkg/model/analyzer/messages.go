//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package analyzer

import (
	"fmt"
)

type Messages struct {
	messages []Message
}

func (m *Messages) String() string {
	result := ""
	for _, msg := range m.messages {
		result += msg.String()
		result += "\n"
	}

	return result
}

func (m *Messages) append(level Level, code int, format string, params ...interface{}) {
	msg := Message{
		Level: level,
		Code: code,
		Content: fmt.Sprintf(format, params...),
	}

	m.messages = append(m.messages, msg)
}

func (m* Messages) emptyField(name string) {
	m.append(Error, 1, "Field cannot be empty: %s", name)
}
