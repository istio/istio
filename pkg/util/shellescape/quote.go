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

package shellescape

import (
	"regexp"
	"strings"
)

var unsafeValue = regexp.MustCompile(`[^\\w@%+=:,./-]`)

func Quote(s string) string {
	// ported from https://github.com/chrissimpkins/shellescape/blob/master/lib/shellescape/main.py
	if len(s) == 0 {
		return "''"
	}

	if unsafeValue.MatchString(s) {
		return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
	}

	return s
}
