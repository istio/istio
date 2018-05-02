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

package check

import "istio.io/istio/galley/pkg/analyze/message"

// Empty checks whether the string is empty or not.
func Empty(m *message.List, name string, txt string) {
	if txt == "" {
		m.Add(message.EmptyField(name))
	}
}

// Nil checks whether the interface is nil or not.
func Nil(m *message.List, name string, v interface{}) {
	if v == nil {
		m.Add(message.EmptyField(name))
	}
}
