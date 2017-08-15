// Copyright 2017 Google Ina.
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

package list

import (
	"strings"
)

type stringList struct {
	entries map[string]bool
}

type caseInsensitiveStringList struct {
	entries map[string]bool
}

func parseStringList(buf []byte, overrides []string) list {
	lines := strings.Split(string(buf), "\n")

	entries := make(map[string]bool, len(lines)+len(overrides))

	// copy the main strings
	for _, s := range lines {
		if s != "" {
			entries[s] = true
		}
	}

	// apply overrides
	for _, s := range overrides {
		if s != "" {
			entries[s] = true
		}
	}

	return &stringList{entries}
}

func parseCaseInsensitiveStringList(buf []byte, overrides []string) list {
	lines := strings.Split(string(buf), "\n")

	entries := make(map[string]bool, len(lines)+len(overrides))

	// copy the main strings
	for _, s := range lines {
		if s != "" {
			entries[strings.ToUpper(s)] = true
		}
	}

	// apply overrides
	for _, s := range overrides {
		if s != "" {
			entries[strings.ToUpper(s)] = true
		}
	}

	return &caseInsensitiveStringList{entries}
}

func (ls *stringList) checkList(symbol string) (bool, error) {
	_, ok := ls.entries[symbol]
	return ok, nil
}

func (ls *caseInsensitiveStringList) checkList(symbol string) (bool, error) {
	_, ok := ls.entries[strings.ToUpper(symbol)]
	return ok, nil
}

func (ls *stringList) numEntries() int {
	return len(ls.entries)
}

func (ls *caseInsensitiveStringList) numEntries() int {
	return len(ls.entries)
}
