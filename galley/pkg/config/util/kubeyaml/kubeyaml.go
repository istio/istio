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

package kubeyaml

import (
	"bytes"
	"strings"
)

const (
	yamlSeparator = "---\n"
)

// Split the given yaml doc if it's multipart document.
func Split(yamlText []byte) [][]byte {
	parts := bytes.Split(yamlText, []byte(yamlSeparator))
	var result [][]byte
	for _, p := range parts {
		if len(p) != 0 {
			result = append(result, p)
		}
	}
	return result
}

// SplitString splits the given yaml doc if it's multipart document.
func SplitString(yamlText string) []string {
	parts := strings.Split(yamlText, yamlSeparator)
	var result []string
	for _, p := range parts {
		if len(p) != 0 {
			result = append(result, p)
		}
	}
	return result
}

// Join the given yaml parts into a single multipart document.
func Join(parts ...[]byte) []byte {
	var b bytes.Buffer

	var lastIsNewLine bool
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		if b.Len() != 0 {
			if !lastIsNewLine {
				_, _ = b.WriteString("\n")
			}
			b.WriteString(yamlSeparator)
		}
		_, _ = b.Write(p)
		s := string(p)
		lastIsNewLine = s[len(s) - 1] == '\n'
	}

	return b.Bytes()
}

// JoinString joins the given yaml parts into a single multipart document.
func JoinString(parts ...string) string {
	var st strings.Builder

	var lastIsNewLine bool
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		if st.Len() != 0 {
			if !lastIsNewLine {
				_, _ = st.WriteString("\n")
			}
			st.WriteString(yamlSeparator)
		}
		_, _ = st.WriteString(p)
		lastIsNewLine = p[len(p) - 1] == '\n'
	}

	return st.String()
}
