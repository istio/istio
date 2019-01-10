// Copyright 2018 Istio Authors
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

package env

import (
	"os"
	"strconv"
	"time"
)

// Integer returns the integer value of an environment variable. The default
// value is returned if the environment variable is not set or could not be
// parsed as an integer.
func Integer(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		if a, err := strconv.Atoi(v); err == nil {
			return a
		}
	}
	return def
}

// Duration returns the duration value of an environment variable. The default
// value is returned if the environment variable is not set or could not be
// parsed as a valid duration.
func Duration(name string, def time.Duration) time.Duration {
	if v := os.Getenv(name); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
