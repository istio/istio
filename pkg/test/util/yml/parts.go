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

package yml

import (
	"bytes"
	"strings"
)

const (
	yamlSeparator = "\n---\n"
)

// Split the given yaml doc if it's multipart document.
func Split(yamlText []byte) [][]byte {
	return bytes.Split(yamlText, []byte(yamlSeparator))
}

// SplitString splits the given yaml doc if it's multipart document.
func SplitString(yamlText string) []string {
	return strings.Split(yamlText, yamlSeparator)
}

// Join the given yaml parts into a single multipart document.
func Join(parts ...[]byte) []byte {
	return bytes.Join(parts, []byte(yamlSeparator))
}

// JoinString joins the given yaml parts into a single multipart document.
func JoinString(parts ...string) string {
	return strings.Join(parts, yamlSeparator)
}
