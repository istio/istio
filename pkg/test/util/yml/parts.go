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
	"bufio"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	joinSeparator = "\n---\n"
)

// SplitYamlByKind splits the given YAML into parts indexed by kind.
func SplitYamlByKind(content string) map[string]string {
	cfgs := SplitString(content)
	result := map[string]string{}
	for _, cfg := range cfgs {
		var typeMeta metav1.TypeMeta
		if e := yaml.Unmarshal([]byte(cfg), &typeMeta); e != nil {
			// Ignore invalid parts. This most commonly happens when it's empty or contains only comments.
			continue
		}
		result[typeMeta.Kind] = JoinString(result[typeMeta.Kind], cfg)
	}
	return result
}

// SplitYamlByKind splits the given YAML into parts indexed by kind.
func GetMetadata(content string) []metav1.ObjectMeta {
	cfgs := SplitString(content)
	result := []metav1.ObjectMeta{}
	for _, cfg := range cfgs {
		var m metav1.ObjectMeta
		if e := yaml.Unmarshal([]byte(cfg), &m); e != nil {
			// Ignore invalid parts. This most commonly happens when it's empty or contains only comments.
			continue
		}
		result = append(result, m)
	}
	return result
}

// SplitString splits the given yaml doc if it's multipart document.
func SplitString(yamlText string) []string {
	out := make([]string, 0)
	reader := bufio.NewReader(strings.NewReader(yamlText))

	parts := []string{}
	active := strings.Builder{}

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if line != "" {
				active.WriteString(line)
			}
			break
		}

		if strings.HasPrefix(line, "---") {
			parts = append(parts, active.String())
			active = strings.Builder{}
		} else {
			active.WriteString(line)
		}
	}

	if active.Len() > 0 {
		parts = append(parts, active.String())
	}

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
