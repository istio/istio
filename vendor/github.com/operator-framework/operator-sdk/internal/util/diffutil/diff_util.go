// Copyright 2018 The Operator-SDK Authors
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

package diffutil

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

func Diff(a, b string) string {
	dmp := diffmatchpatch.New()

	wSrc, wDst, warray := dmp.DiffLinesToRunes(a, b)
	diffs := dmp.DiffMainRunes(wSrc, wDst, false)
	diffs = dmp.DiffCharsToLines(diffs, warray)
	var buff bytes.Buffer
	for _, diff := range diffs {
		text := diff.Text

		switch diff.Type {
		case diffmatchpatch.DiffInsert:
			_, _ = buff.WriteString("\x1b[32m")
			_, _ = buff.WriteString(prefixLines(text, "+"))
			_, _ = buff.WriteString("\x1b[0m")
		case diffmatchpatch.DiffDelete:
			_, _ = buff.WriteString("\x1b[31m")
			_, _ = buff.WriteString(prefixLines(text, "-"))
			_, _ = buff.WriteString("\x1b[0m")
		case diffmatchpatch.DiffEqual:
			_, _ = buff.WriteString(prefixLines(text, " "))
		}
	}
	return buff.String()
}

func prefixLines(s, prefix string) string {
	var buf bytes.Buffer
	lines := strings.Split(s, "\n")
	ls := regexp.MustCompile("^")
	for _, line := range lines[:len(lines)-1] {
		buf.WriteString(ls.ReplaceAllString(line, prefix))
		buf.WriteString("\n")
	}
	return buf.String()
}
