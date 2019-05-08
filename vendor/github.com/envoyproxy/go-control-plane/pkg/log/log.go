// Copyright 2018 Envoyproxy Authors
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.

// Package log provides a logging interface for use in this library.
package log

// Logger interface for reporting informational and warning messages.
type Logger interface {
	// Infof logs a formatted informational message.
	Infof(format string, args ...interface{})

	// Errorf logs a formatted error message.
	Errorf(format string, args ...interface{})
}
