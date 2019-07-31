// Copyright 2019 Istio Authors
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
	"sort"
	"strings"
)

const staticCollectionsTemplate = `
// GENERATED FILE -- DO NOT EDIT
//

package {{.PackageName}}

import (
	"istio.io/istio/galley/pkg/config/collection"
)

var (
{{range .Entries}}
	// {{.VarName}} is the name of collection {{.Name}}
	{{.VarName}} = collection.NewName("{{.Name}}")
{{end}}
)

// CollectionNames returns the collection names declared in this package.
func CollectionNames() []collection.Name {
	return []collection.Name {
		{{range .Entries}}{{.VarName}},
		{{end}}
	}
}
`

type entry struct {
	Name    string
	VarName string
}

// StaticCollections generates a Go file for static-importing Proto packages, so that they get registered statically.
func StaticCollections(packageName string, collections []string) (string, error) {
	var entries []entry

	for _, col := range collections {
		entries = append(entries, entry{Name: col, VarName: asColVarName(col)})
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.Compare(entries[i].Name, entries[j].Name) < 0
	})

	context := struct {
		Entries     []entry
		PackageName string
	}{Entries: entries, PackageName: packageName}

	// Calculate the Go packages that needs to be imported for the proto types to be registered.
	return applyTemplate(staticCollectionsTemplate, context)
}

func asColVarName(n string) string {
	n = camelCase(n, "/")
	n = camelCase(n, ".")
	return n
}

// CamelCase converts the string into camel case string
func CamelCase(s string) string {
	if s == "" {
		return ""
	}
	t := make([]byte, 0, 32)
	i := 0
	if s[0] == '_' {
		// Need a capital letter; drop the '_'.
		t = append(t, 'X')
		i++
	}
	// Invariant: if the next letter is lower case, it must be converted
	// to upper case.
	// That is, we process a word at a time, where words are marked by _ or
	// upper case letter. Digits are treated as words.
	for ; i < len(s); i++ {
		c := s[i]
		if c == '_' && i+1 < len(s) && isASCIILower(s[i+1]) {
			continue // Skip the underscore in s.
		}
		if isASCIIDigit(c) {
			t = append(t, c)
			continue
		}
		// Assume we have a letter now - if not, it's a bogus identifier.
		// The next word is a sequence of characters that must start upper case.
		if isASCIILower(c) {
			c ^= ' ' // Make it a capital letter.
		}
		t = append(t, c) // Guaranteed not lower case.
		// Accept lower case sequence that follows.
		for i+1 < len(s) && isASCIILower(s[i+1]) {
			i++
			t = append(t, s[i])
		}
	}
	return string(t)
}

func camelCase(n string, sep string) string {
	p := strings.Split(n, sep)
	for i := 0; i < len(p); i++ {
		p[i] = CamelCase(p[i])
	}
	return strings.Join(p, "")
}

// Is c an ASCII lower-case letter?
func isASCIILower(c byte) bool {
	return 'a' <= c && c <= 'z'
}

// Is c an ASCII digit?
func isASCIIDigit(c byte) bool {
	return '0' <= c && c <= '9'
}
