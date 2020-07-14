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

package kubeyaml

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"
)

const (
	yamlSeparator = "---\n"
	separator     = "---"
)

// Join the given yaml parts into a single multipart document.
func Join(parts ...[]byte) []byte {
	var b bytes.Buffer

	var lastIsNewLine bool
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		if b.Len() != 0 {
			if !lastIsNewLine {
				_, _ = b.WriteString("\n")
			}
			b.WriteString(yamlSeparator)
		}
		_, _ = b.Write(p)
		s := string(p)
		lastIsNewLine = s[len(s)-1] == '\n'
	}

	return b.Bytes()
}

// JoinString joins the given yaml parts into a single multipart document.
func JoinString(parts ...string) string {
	var st strings.Builder

	var lastIsNewLine bool
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		if st.Len() != 0 {
			if !lastIsNewLine {
				_, _ = st.WriteString("\n")
			}
			st.WriteString(yamlSeparator)
		}
		_, _ = st.WriteString(p)
		lastIsNewLine = p[len(p)-1] == '\n'
	}

	return st.String()
}

type Reader interface {
	Read() ([]byte, error)
}

// YAMLReader adapts from Kubernetes YAMLReader(apimachinery.k8s.io/pkg/util/yaml/decoder.go).
// It records the start line number of the chunk it reads each time.
type YAMLReader struct {
	reader   Reader
	currLine int
}

func NewYAMLReader(r *bufio.Reader) *YAMLReader {
	return &YAMLReader{
		reader:   &LineReader{reader: r},
		currLine: 0,
	}
}

// Read returns a full YAML document, a full YAML document with its values replaced by line numbers and its first line number.
func (r *YAMLReader) Read() ([]byte, []byte, int, error) {
	var buffer bytes.Buffer
	var lineBuffer bytes.Buffer
	startLine := r.currLine + 1
	foundStart := false
	for {
		r.currLine++
		line, err := r.reader.Read()
		if err != nil && err != io.EOF {
			return nil, nil, startLine, err
		}

		// detect beginning of the chunk
		if !bytes.Equal(line, []byte("\n")) && !bytes.Equal(line, []byte(yamlSeparator)) && !foundStart {
			startLine = r.currLine
			foundStart = true
		}

		sep := len([]byte(separator))
		if i := bytes.Index(line, []byte(separator)); i == 0 {
			// We have a potential document terminator
			i += sep
			after := line[i:]
			if len(strings.TrimRightFunc(string(after), unicode.IsSpace)) == 0 {
				if buffer.Len() != 0 {
					return buffer.Bytes(), lineBuffer.Bytes(), startLine, nil
				}
				if err == io.EOF {
					return nil, nil, startLine, err
				}
			}
		}
		if err == io.EOF {
			if buffer.Len() != 0 {
				// If we're at EOF, we have a final, non-terminated line. Return it.
				return buffer.Bytes(), lineBuffer.Bytes(), startLine, nil
			}
			return nil, nil, startLine, err
		}

		lineWithNumber := []byte(ConvertToLineNumber(string(line), r.currLine))

		buffer.Write(line)
		lineBuffer.Write(lineWithNumber)
	}
}

type LineReader struct {
	reader *bufio.Reader
}

// Read returns a single line (with '\n' ended) from the underlying reader.
// An error is returned iff there is an error with the underlying reader.
func (r *LineReader) Read() ([]byte, error) {
	var (
		isPrefix bool  = true
		err      error = nil
		line     []byte
		buffer   bytes.Buffer
	)

	for isPrefix && err == nil {
		line, isPrefix, err = r.reader.ReadLine()
		buffer.Write(line)
	}
	buffer.WriteByte('\n')
	return buffer.Bytes(), err
}

// ConvertToLineNumber converts value in the field to its line number
func ConvertToLineNumber(line string, lineNumber int) string {
	resStr := line

	// Find index of ':' and '#'
	colonInd := strings.Index(resStr, ":")
	sharpInd := strings.Index(resStr, " #")

	// Skip line with only comments
	trimStr := strings.TrimSpace(resStr)
	if len(trimStr) > 0 && trimStr[0] == '#' || len(trimStr) == 0{ return resStr }

	// Remove comments after the line
	if sharpInd > 0 {
		resStr = resStr[:sharpInd] + "\n"
	}

	// Handle the edge case that ":" in the comment
	if colonInd > sharpInd && sharpInd > 0 {colonInd = -1}

	// If string has ':', change value after ':' to line number
	if colonInd > 0 && colonInd != len(strings.TrimRight(resStr, " \n")) - 1 {

		fieldValueToStr := fmt.Sprintf("%d", lineNumber)
		resStr = resStr[:colonInd] + ": " + fieldValueToStr + "\n"

	}else if colonInd < 0 {

		// If the value is in a field with the array form, change it to line number
		spaceInd := 0
		for resStr[spaceInd] == ' ' || (resStr[spaceInd] == '-'&& spaceInd != len(resStr) - 1 && resStr[spaceInd + 1] == ' ') {
			spaceInd++
		}
		resStr = resStr[:spaceInd] + fmt.Sprintf("%d", lineNumber) + "\n"
	}

	return resStr
}

// JsonUnmarshal perform json.Unmarshal() to the right type
func JsonUnmarshal (item []byte) interface{} {
	var float     float64
	var mapObject map[string] interface{}
	var str       string
	var arr       []interface{}
	var b         bool

	err := json.Unmarshal(item, &float)
	if err == nil {
		return float
	}
	err = json.Unmarshal(item, &mapObject)
	if err == nil {
		return mapObject
	}
	err = json.Unmarshal(item, &str)
	if err == nil {
		return str
	}
	err = json.Unmarshal(item, &arr)
	if err == nil {
		return arr
	}
	err = json.Unmarshal(item, &b)
	if err == nil {
		return b
	}
	return nil
}
