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
	"bytes"
	"sort"
	"text/template"
)

const importInitTemplate = `
// GENERATED FILE -- DO NOT EDIT
//

package {{.PackageName}}

import (
	// Pull in all the known proto types to ensure we get their types registered.

{{range .Packages}}
	// Register protos in "{{.}}"
	_ "{{.}}"
{{end}}
)
`

// StaticInit generates a Go file for static-importing Proto packages, so that they get registered statically.
func StaticInit(packageName string, packages []string) (string, error) {
	// Single instance and sort names
	names := make(map[string]struct{})

	for _, p := range packages {
		if p != "" {
			names[p] = struct{}{}
		}
	}

	sorted := make([]string, 0, len(names))
	for p := range names {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)

	context := struct {
		Packages    []string
		PackageName string
	}{Packages: sorted, PackageName: packageName}

	return applyTemplate(importInitTemplate, context)
}

func applyTemplate(tmpl string, i interface{}) (string, error) {
	t := template.New("tmpl")

	t2 := template.Must(t.Parse(tmpl))

	var b bytes.Buffer
	if err := t2.Execute(&b, i); err != nil {
		return "", err
	}

	return b.String(), nil
}
