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

package util

import (
	"fmt"
	"net/url"
	"strings"

	"istio.io/pkg/log"
)

var (
	scope = log.RegisterScope("util", "util", 0)
)

// IsFilePath reports whether the given URL is a local file path.
func IsFilePath(path string) bool {
	return strings.Contains(path, "/") || strings.Contains(path, ".")
}

// IsHTTPURL checks whether the given URL is a HTTP URL.
func IsHTTPURL(path string) (bool, error) {
	u, err := url.Parse(path)
	valid := err == nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https")
	if strings.HasPrefix(path, "http") && !valid {
		return false, fmt.Errorf("%s starts with http but is not a valid URL: %s", path, err)
	}
	return valid, nil
}
