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

import "istio.io/istio/galley/pkg/config/resource"

// Template is a message template
type Template struct {
	// The error code of the message
	Code Code

	// The level of the message.
	Level Level

	// TODO: Make this localizable
	template string
}

// NewTemplate creates a new Message as a template
func NewTemplate(l Level, c Code, template string) Template {
	return Template{
		Level:    l,
		Code:     c,
		template: template,
	}
}

// Apply origin and parameters to this template and return the result.
func (t *Template) Apply(o resource.Origin, p ...interface{}) Message {
	return Message{
		Level:      t.Level,
		Code:       t.Code,
		template:   t.template,
		Origin:     o,
		Parameters: p,
	}
}
