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

package matcher

import (
	"strings"

	uri_template "github.com/envoyproxy/go-control-plane/envoy/extensions/path/match/uri_template/v3"
)

// TODO(jaellio): Define elsewhere? In utils?
var matchOneTemplate = "{*}"
var matchAnyTemplate = "{**}"

// PatherTemplateMatcher creates a URI path matcher for path.
func PathTemplateMatcher(path string) *uri_template.UriTemplateMatchConfig {
	return &uri_template.UriTemplateMatchConfig{
		PathTemplate: sanitizePathTemplate(path),
	}
}

// TODO(jaellio): add description
// If path contains "{*}", it will be replaced with "*".
// If path contains "{**}", it will be replaced with "**".
// If the path already contained "*" or "**", they will be left as is.
func sanitizePathTemplate(path string) string {
	if strings.Contains(path, matchOneTemplate) {
		path = strings.ReplaceAll(path, matchOneTemplate, "*")
	}
	if strings.Contains(path, matchAnyTemplate) {
		path = strings.ReplaceAll(path, matchAnyTemplate, "**")
	}
	return path
}

// TODO(jaellio): should this go in util and be exported? Need a comment?
func IsPathTemplate(value string) bool {
	return strings.Contains(value, matchOneTemplate) || strings.Contains(value, matchAnyTemplate)
}
