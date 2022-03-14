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
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gogo/protobuf/types"
)

type FileFilter func(fileName string) bool

// StringBoolMapToSlice creates and returns a slice of all the map keys with true.
func StringBoolMapToSlice(m map[string]bool) []string {
	s := make([]string, 0, len(m))
	for k, v := range m {
		if v {
			s = append(s, k)
		}
	}
	return s
}

// ReadFilesWithFilter reads files from path, for a directory it recursively reads files and filters the results
// for single file it directly reads the file. It returns a concatenated output of all matching files' content.
func ReadFilesWithFilter(path string, filter FileFilter) (string, error) {
	fileList, err := FindFiles(path, filter)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, file := range fileList {
		a, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		if _, err := sb.WriteString(string(a) + "\n"); err != nil {
			return "", err
		}
	}
	return sb.String(), nil
}

// FindFiles reads files from path, and returns the file names that match the filter.
func FindFiles(path string, filter FileFilter) ([]string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	var fileList []string
	if fi.IsDir() {
		err := filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !filter(path) {
				return nil
			}
			fileList = append(fileList, path)
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		fileList = append(fileList, path)
	}
	return fileList, nil
}

// ParseValue parses string into a value
func ParseValue(valueStr string) interface{} {
	var value interface{}
	if v, err := strconv.Atoi(valueStr); err == nil {
		value = v
	} else if v, err := strconv.ParseFloat(valueStr, 64); err == nil {
		value = v
	} else if v, err := strconv.ParseBool(valueStr); err == nil {
		value = v
	} else {
		value = strings.ReplaceAll(valueStr, "\\,", ",")
	}
	return value
}

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

// RenderTemplate is a helper method to render a template with the given values.
func RenderTemplate(tmpl string, ts interface{}) (string, error) {
	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return "", err
	}
	buf := new(bytes.Buffer)
	err = t.Execute(buf, ts)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func ValueString(v *types.Value) string {
	switch x := v.Kind.(type) {
	case *types.Value_StringValue:
		return x.StringValue
	case *types.Value_NumberValue:
		return fmt.Sprint(x.NumberValue)
	default:
		return v.String()
	}
}
