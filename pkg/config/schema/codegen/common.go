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

package codegen

import (
	"bytes"
	"strings"
	"text/template"
)

const (
	commentLinePrefix = "// "
)

func applyTemplate(tmpl string, i interface{}) (string, error) {
	t := template.New("tmpl").Funcs(template.FuncMap{
		"wordWrap":     wordWrap,
		"commentBlock": commentBlock,
		"hasPrefix":    strings.HasPrefix,
	})

	t2 := template.Must(t.Parse(tmpl))

	var b bytes.Buffer
	if err := t2.Execute(&b, i); err != nil {
		return "", err
	}

	return b.String(), nil
}

func commentBlock(in []string, indentTabs int) string {
	// Copy the input array.
	in = append([]string{}, in...)

	// Apply the tabs and comment prefix to each line.
	for lineIndex := range in {
		prefix := ""
		for tabIndex := 0; lineIndex > 0 && tabIndex < indentTabs; tabIndex++ {
			prefix += "\t"
		}
		prefix += commentLinePrefix
		in[lineIndex] = prefix + in[lineIndex]
	}

	// Join the lines with carriage returns.
	return strings.Join(in, "\n")
}

func wordWrap(in string, maxLineLength int) []string {
	// First, split the input based on any user-created lines (i.e. the string contains "\n").
	inputLines := strings.Split(in, "\n")
	outputLines := make([]string, 0)

	line := ""
	for i, inputLine := range inputLines {
		if i > 0 {
			// Process a user-defined carriage return.
			outputLines = append(outputLines, line)
			line = ""
		}

		words := strings.Split(inputLine, " ")

		for len(words) > 0 {
			// Take the next word.
			word := words[0]
			words = words[1:]

			if len(line)+len(word) > maxLineLength {
				// Need to word wrap - emit the current line.
				outputLines = append(outputLines, line)
				line = ""
			}

			// Add the word to the current line.
			if len(line) > 0 {
				line += " "
			}
			line += word
		}
	}

	// Emit the final line
	outputLines = append(outputLines, line)

	return outputLines
}
