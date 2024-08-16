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
	"strings"
)

// ConsolidateLog is a helper function to dedup the log message.
func ConsolidateLog(logMessage string) string {
	logCountMap := make(map[string]int)
	stderrSlice := strings.Split(logMessage, "\n")
	for _, item := range stderrSlice {
		if item == "" {
			continue
		}
		_, exist := logCountMap[item]
		if exist {
			logCountMap[item]++
		} else {
			logCountMap[item] = 1
		}
	}
	var sb strings.Builder
	for _, item := range stderrSlice {
		if logCountMap[item] == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("%s (repeated %v times)\n", item, logCountMap[item]))
		// reset seen log count
		logCountMap[item] = 0
	}
	return sb.String()
}
