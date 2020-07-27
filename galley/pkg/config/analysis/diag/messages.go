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

package diag

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ghodss/yaml"
)

const (
	LogFormat  = "log"
	JSONFormat = "json"
	YAMLFormat = "yaml"
)

var (
	MsgOutputFormatKeys = []string{LogFormat, JSONFormat, YAMLFormat}
	MsgOutputFormats    = make(map[string]bool)
)

func init() {
	sort.Strings(MsgOutputFormatKeys)
	for _, key := range MsgOutputFormatKeys {
		MsgOutputFormats[key] = true
	}
}

// Messages is a slice of Message items.
type Messages []Message

// Add a new message to the messages
func (ms *Messages) Add(m Message) {
	*ms = append(*ms, m)
}

// Sort the message lexicographically by level, code, resource origin name, then string.
func (ms *Messages) Sort() {
	sort.Slice(*ms, func(i, j int) bool {
		a, b := (*ms)[i], (*ms)[j]
		switch {
		case a.Type.Level() != b.Type.Level():
			return a.Type.Level().sortOrder < b.Type.Level().sortOrder
		case a.Type.Code() != b.Type.Code():
			return a.Type.Code() < b.Type.Code()
		case a.Resource == nil && b.Resource != nil:
			return true
		case a.Resource != nil && b.Resource == nil:
			return false
		case a.Resource != nil && b.Resource != nil && a.Resource.Origin.FriendlyName() != b.Resource.Origin.FriendlyName():
			return a.Resource.Origin.FriendlyName() < b.Resource.Origin.FriendlyName()
		default:
			return a.String() < b.String()
		}
	})
}

// SortedDedupedCopy returns a different sorted (and deduped) Messages struct.
func (ms *Messages) SortedDedupedCopy() Messages {
	newMs := append((*ms)[:0:0], *ms...)
	newMs.Sort()

	// Take advantage of the fact that the list is already sorted to dedupe
	// messages (any duplicates should be adjacent).
	var deduped Messages
	for _, m := range newMs {
		// Two messages are duplicates if they have the same string representation.
		if len(deduped) != 0 && deduped[len(deduped)-1].String() == m.String() {
			continue
		}
		deduped = append(deduped, m)
	}
	return deduped
}

// SetDocRef sets the doc URL reference tracker for the messages
func (ms *Messages) SetDocRef(docRef string) *Messages {
	for i := range *ms {
		(*ms)[i].DocRef = docRef
	}
	return ms
}

// Filter only keeps messages at or above the specified output level
func (ms *Messages) Filter(outputLevel Level) Messages {
	var outputMessages Messages
	for _, m := range *ms {
		if m.Type.Level().IsWorseThanOrEqualTo(outputLevel) {
			outputMessages = append(outputMessages, m)
		}
	}
	return outputMessages
}

// Print output messages in the specified format with color options
func (ms *Messages) Print(format string, colorize bool) (string, error) {
	switch format {
	case LogFormat:
		return ms.PrintLog(colorize)
	case JSONFormat:
		return ms.PrintJSON()
	case YAMLFormat:
		return ms.PrintYAML()
	default:
		return "", fmt.Errorf("invalid format, expected one of %v but got %q", MsgOutputFormatKeys, format)
	}
}

// PrintLog outputs messages in the log format
func (ms *Messages) PrintLog(colorize bool) (string, error) {
	var logOutput []string
	for _, m := range *ms {
		logOutput = append(logOutput, m.Render(colorize))
	}
	return strings.Join(logOutput, "\n"), nil
}

// PrintJSON outputs messages in the json format
func (ms *Messages) PrintJSON() (string, error) {
	jsonOutput, err := json.MarshalIndent(*ms, "", "\t")
	return string(jsonOutput), err
}

// PrintYAML outputs messages in the yaml format
func (ms *Messages) PrintYAML() (string, error) {
	yamlOutput, err := yaml.Marshal(*ms)
	return string(yamlOutput), err
}
