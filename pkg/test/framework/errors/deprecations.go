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

package errors

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/hashicorp/go-multierror"
)

type DeprecatedError struct {
	msg string
}

func NewDeprecatedError(format string, args ...interface{}) error {
	return &DeprecatedError{fmt.Sprintf(format, args...)}
}

func IsDeprecatedError(err error) bool {
	_, ok := err.(*DeprecatedError)
	return ok
}

func IsOrContainsDeprecatedError(err error) bool {
	if IsDeprecatedError(err) {
		return true
	}

	if m, ok := err.(*multierror.Error); ok {
		for _, e := range m.Errors {
			if IsDeprecatedError(e) {
				return true
			}
		}
	}

	return false
}

func (de *DeprecatedError) Error() string {
	return de.msg
}

// FindDeprecatedMessagesInEnvoyLog looks for deprecated messages in the `logs` parameter. If found, it will return
// a DeprecatedError. Use `extraInfo` to pass additional info, like pod namespace/name, etc.
func FindDeprecatedMessagesInEnvoyLog(logs, extraInfo string) error {
	scanner := bufio.NewScanner(strings.NewReader(logs))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(strings.ToLower(line), "deprecated") {
			if len(extraInfo) > 0 {
				extraInfo = fmt.Sprintf(" (%s)", extraInfo)
			}
			return NewDeprecatedError("usage of deprecated stuff in Envoy%s: %s", extraInfo, line)
		}
	}

	return nil
}
