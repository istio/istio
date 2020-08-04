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

package yml

import (
	"regexp"
	"strings"

	"github.com/ghodss/yaml"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	joinSeparator = "\n---\n"
)

var (
	// Split where the '---' appears at the very beginning of a line. This will avoid
	// accidentally splitting in cases where yaml resources contain nested yaml (which
	// is indented).
	splitRegex = regexp.MustCompile(`(^|\n)---`)
)

// SplitYamlByKind splits the given YAML into parts indexed by kind.
func SplitYamlByKind(content string) map[string]string {
	cfgs := SplitString(content)
	result := map[string]string{}
	for _, cfg := range cfgs {
		var typeMeta kubeApiMeta.TypeMeta
		if e := yaml.Unmarshal([]byte(cfg), &typeMeta); e != nil {
			// Ignore invalid parts. This most commonly happens when it's empty or contains only comments.
			continue
		}
		result[typeMeta.Kind] = JoinString(result[typeMeta.Kind], cfg)
	}
	return result
}

// SplitString splits the given yaml doc if it's multipart document.
func SplitString(yamlText string) []string {
	out := make([]string, 0)
	parts := splitRegex.Split(yamlText, -1)
	for _, part := range parts {
		part := strings.TrimSpace(part)
		if len(part) > 0 {
			out = append(out, part)
		}
	}
	return out
}

// JoinString joins the given yaml parts into a single multipart document.
func JoinString(parts ...string) string {
	// Assume that each part is already a multi-document. Split and trim each part,
	// if necessary.
	toJoin := make([]string, 0, len(parts))
	for _, part := range parts {
		toJoin = append(toJoin, SplitString(part)...)
	}

	return strings.Join(toJoin, joinSeparator)
}
