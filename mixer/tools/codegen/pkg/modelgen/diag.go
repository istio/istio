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

package modelgen

import (
	"bytes"
	"fmt"
	"strings"
)

type (
	diagKind uint8
)

const (
	errorDiag diagKind = iota
)

const (
	unknownFile = ""
	unknownLine = ""
)

type (
	// Diag represents diagnostic information.
	diag struct {
		kind     diagKind
		location location
		message  string
	}
	// location represents the location of the Diag
	location struct {
		file string
		// TODO: Currently Line is always set as UNKNOWN_LINE. Consider using proto's
		// SourceCodeInfo to exactly point to the line number.
		line string
	}
)

func (diag diag) String() string {
	var kind string
	if diag.kind == errorDiag {
		kind = "Error"
	}

	msg := strings.TrimSpace(diag.message)
	if !strings.HasSuffix(msg, ".") {
		msg += "."
	}

	if diag.location.line != "" {
		return fmt.Sprintf("%s: %s:%s: %s\n", kind, diag.location.file, diag.location.line, msg)
	} else if diag.location.file != "" {
		return fmt.Sprintf("%s: %s: %s\n", kind, diag.location.file, msg)
	} else {
		return fmt.Sprintf("%s: %s\n", kind, msg)
	}
}

func stringifyDiags(diags []diag) string {
	var result bytes.Buffer
	for _, d := range diags {
		result.WriteString(d.String())
	}
	return result.String()
}

func (m *Model) addError(file string, line string, format string, a ...interface{}) {
	m.diags = append(m.diags, createError(file, line, format, a...))
}

func createError(file string, line string, format string, a ...interface{}) diag {
	if len(a) == 0 {
		return diag{kind: errorDiag, location: location{file: file, line: line}, message: format}
	}

	return diag{kind: errorDiag, location: location{file: file, line: line}, message: fmt.Sprintf(format, a...)}
}
