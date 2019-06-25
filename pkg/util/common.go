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

package util

import (
	"strings"

	"gopkg.in/yaml.v2"
)

const (
	// LocalFilePrefix is a prefix for local files.
	LocalFilePrefix = "file:///"
)

// Tree is a tree.
type Tree map[string]interface{}

// String implements the Stringer interface method.
func (t Tree) String() string {
	y, err := yaml.Marshal(t)
	if err != nil {
		return err.Error()
	}
	return string(y)
}

// IsFilePath reports whether the given URL is a local file path.
func IsFilePath(path string) bool {
	return strings.HasPrefix(path, LocalFilePrefix)
}

// GetLocalFilePath returns the local file path string of the form /a/b/c, given a file URL of the form file:///a/b/c
func GetLocalFilePath(path string) string {
	// LocalFilePrefix always starts with file:/// but this includes the absolute path leading slash, preserve that.
	return "/" + strings.TrimPrefix(path, LocalFilePrefix)
}
