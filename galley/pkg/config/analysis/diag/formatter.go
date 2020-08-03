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
	"strings"

	"github.com/ghodss/yaml"
)

// Print output messages in the specified format with color options
func Print(ms Messages, format string, colorize bool) (string, error) {
	switch format {
	case LogFormat:
		return PrintLog(ms, colorize)
	case JSONFormat:
		return PrintJSON(ms)
	case YAMLFormat:
		return PrintYAML(ms)
	default:
		return "", fmt.Errorf("invalid format, expected one of %v but got %q", MsgOutputFormatKeys, format)
	}
}

// PrintLog outputs messages in the log format
func PrintLog(ms Messages, colorize bool) (string, error) {
	var logOutput []string
	for _, m := range ms {
		logOutput = append(logOutput, m.Render(colorize))
	}
	return strings.Join(logOutput, "\n"), nil
}

// PrintJSON outputs messages in the json format
func PrintJSON(ms Messages) (string, error) {
	jsonOutput, err := json.MarshalIndent(ms, "", "\t")
	return string(jsonOutput), err
}

// PrintYAML outputs messages in the yaml format
func PrintYAML(ms Messages) (string, error) {
	yamlOutput, err := yaml.Marshal(ms)
	return string(yamlOutput), err
}
