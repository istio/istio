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

package data

import (
	"bytes"
	"fmt"
)

// Logger is used to capture the events that happen within fake adapters & templates during testing.
type Logger struct {
	b bytes.Buffer
}

// Write s to the log, with a prefix of name. A newline character is added.
func (l *Logger) Write(name string, s string) {
	if l != nil {
		fmt.Fprintf(&l.b, "[%s] %s\n", name, s)
	}
}

// WriteFormat writes a formatted string to the log, with a prefix of name. A newline character is added.
func (l *Logger) WriteFormat(name string, format string, args ...interface{}) {
	if l != nil {
		s := fmt.Sprintf(format, args...)
		l.Write(name, s)
	}
}

// Clear the contents of this logger. Useful for reducing the event output to Write more readable tests.
func (l *Logger) Clear() {
	if l != nil {
		l.b.Reset()
	}
}

// String returns the current contents of the log.
func (l *Logger) String() string {
	if l == nil {
		return ""
	}

	return l.b.String()
}
