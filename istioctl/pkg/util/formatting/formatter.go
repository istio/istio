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

package formatting

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/pkg/env"
)

// Formatting options for Messages
const (
	LogFormat  = "log"
	JSONFormat = "json"
	YAMLFormat = "yaml"
)

var (
	MsgOutputFormatKeys = []string{LogFormat, JSONFormat, YAMLFormat}
	MsgOutputFormats    = make(map[string]bool)
	termEnvVar          = env.Register("TERM", "", "Specifies terminal type.  Use 'dumb' to suppress color output")
)

func init() {
	for _, key := range MsgOutputFormatKeys {
		MsgOutputFormats[key] = true
	}
}

// Print output messages in the specified format with color options
func Print(ms diag.Messages, format string, colorize bool) (string, error) {
	switch format {
	case LogFormat:
		return printLog(ms, colorize), nil
	case JSONFormat:
		return printJSON(ms)
	case YAMLFormat:
		return printYAML(ms)
	default:
		return "", fmt.Errorf("invalid format, expected one of %v but got %q", MsgOutputFormatKeys, format)
	}
}

func printLog(ms diag.Messages, colorize bool) string {
	var logOutput []string
	for _, m := range ms {
		logOutput = append(logOutput, render(m, colorize))
	}
	return strings.Join(logOutput, "\n")
}

func printJSON(ms diag.Messages) (string, error) {
	jsonOutput, err := json.MarshalIndent(ms, "", "\t")
	return string(jsonOutput), err
}

func printYAML(ms diag.Messages) (string, error) {
	yamlOutput, err := yaml.Marshal(ms)
	return string(yamlOutput), err
}

// Formatting options for Message
var (
	colorPrefixes = map[diag.Level]string{
		diag.Info:    "",           // no special color for info messages
		diag.Warning: "\033[33m",   // yellow
		diag.Error:   "\033[1;31m", // bold red
	}
)

// render turns a Message instance into a string with an option of colored bash output
func render(m diag.Message, colorize bool) string {
	return fmt.Sprintf("%s%v%s [%v]%s %s",
		colorPrefix(m, colorize), m.Type.Level(), colorSuffix(colorize),
		m.Type.Code(), m.Origin(), fmt.Sprintf(m.Type.Template(), m.Parameters...),
	)
}

func colorPrefix(m diag.Message, colorize bool) string {
	if !colorize {
		return ""
	}

	prefix, ok := colorPrefixes[m.Type.Level()]
	if !ok {
		return ""
	}
	return prefix
}

func colorSuffix(colorize bool) string {
	if !colorize {
		return ""
	}
	return "\033[0m"
}

func IstioctlColorDefault(writer io.Writer) bool {
	if strings.EqualFold(termEnvVar.Get(), "dumb") {
		return false
	}

	file, ok := writer.(*os.File)
	if ok {
		if !isatty.IsTerminal(file.Fd()) {
			return false
		}
	}

	return true
}
