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
	"fmt"
	"sort"
	"strings"

	"istio.io/istio/pkg/config/schema/ast"
)

const staticResourceTemplate = `
// GENERATED FILE -- DO NOT EDIT
//

package {{.PackageName}}

import (
	"istio.io/istio/pkg/config/schema/collections"
)

var (
{{- range .Entries }}
	{{.Resource.Kind}} = collections.{{ .Collection.VariableName }}.Resource().GroupVersionKind()
{{- end }}
)
`

const staticCollectionsTemplate = `
// GENERATED FILE -- DO NOT EDIT
//

package {{.PackageName}}

import (
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/config/validation"
)

var (
{{ range .Entries }}
	{{ commentBlock (wordWrap (printf "%s %s" .Collection.VariableName .Collection.Description) 70) 1 }}
	{{ .Collection.VariableName }} = collection.Builder {
		Name: "{{ .Collection.Name }}",
		VariableName: "{{ .Collection.VariableName }}",
		Disabled: {{ .Collection.Disabled }},
		Resource: resource.Builder {
			Group: "{{ .Resource.Group }}",
			Kind: "{{ .Resource.Kind }}",
			Plural: "{{ .Resource.Plural }}",
			Version: "{{ .Resource.Version }}",
			Proto: "{{ .Resource.Proto }}",
			ProtoPackage: "{{ .Resource.ProtoPackage }}",
			ClusterScoped: {{ .Resource.ClusterScoped }},
			ValidateProto: validation.{{ .Resource.Validate }},
		}.MustBuild(),
	}.MustBuild()
{{ end }}

	// All contains all collections in the system.
	All = collection.NewSchemasBuilder().
	{{- range .Entries }}
		MustAdd({{ .Collection.VariableName }}).
	{{- end }}
		Build()

	// Istio contains only Istio collections.
	Istio = collection.NewSchemasBuilder().
	{{- range .Entries }}
		{{- if (hasPrefix .Collection.Name "istio/") }}
		MustAdd({{ .Collection.VariableName }}).
		{{- end}}
	{{- end }}
		Build()

	// Kube contains only kubernetes collections.
	Kube = collection.NewSchemasBuilder().
	{{- range .Entries }}
		{{- if (hasPrefix .Collection.Name "k8s/") }}
		MustAdd({{ .Collection.VariableName }}).
		{{- end }}
	{{- end }}
		Build()

	// Pilot contains only collections used by Pilot.
	Pilot = collection.NewSchemasBuilder().
	{{- range .Entries }}
		{{- if .Collection.Pilot }}
		MustAdd({{ .Collection.VariableName }}).
		{{- end}}
	{{- end }}
		Build()

	// PilotServiceApi contains only collections used by Pilot, including experimental Service Api.
	PilotServiceApi = collection.NewSchemasBuilder().
	{{- range .Entries }}
		{{- if or (.Collection.Pilot) (hasPrefix .Collection.Name "k8s/service_apis") }}
		MustAdd({{ .Collection.VariableName }}).
		{{- end}}
	{{- end }}
		Build()

	// Deprecated contains only collections used by that will soon be used by nothing.
	Deprecated = collection.NewSchemasBuilder().
	{{- range .Entries }}
		{{- if .Collection.Deprecated }}
		MustAdd({{ .Collection.VariableName }}).
		{{- end}}
	{{- end }}
		Build()
)
`

type colEntry struct {
	Collection *ast.Collection
	Resource   *ast.Resource
}

func WriteGvk(packageName string, m *ast.Metadata) (string, error) {
	entries := make([]colEntry, 0, len(m.Collections))
	for _, c := range m.Collections {
		if !c.Pilot {
			continue
		}
		r := m.FindResourceForGroupKind(c.Group, c.Kind)
		if r == nil {
			return "", fmt.Errorf("failed to find resource (%s/%s) for collection %s", c.Group, c.Kind, c.Name)
		}

		entries = append(entries, colEntry{
			Collection: c,
			Resource:   r,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return strings.Compare(entries[i].Collection.Name, entries[j].Collection.Name) < 0
	})

	context := struct {
		Entries     []colEntry
		PackageName string
	}{
		Entries:     entries,
		PackageName: packageName,
	}

	// Calculate the Go packages that needs to be imported for the proto types to be registered.
	return applyTemplate(staticResourceTemplate, context)
}

// StaticCollections generates a Go file for static-importing Proto packages, so that they get registered statically.
func StaticCollections(packageName string, m *ast.Metadata) (string, error) {
	entries := make([]colEntry, 0, len(m.Collections))
	for _, c := range m.Collections {
		r := m.FindResourceForGroupKind(c.Group, c.Kind)
		if r == nil {
			return "", fmt.Errorf("failed to find resource (%s/%s) for collection %s", c.Group, c.Kind, c.Name)
		}

		entries = append(entries, colEntry{
			Collection: c,
			Resource:   r,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return strings.Compare(entries[i].Collection.Name, entries[j].Collection.Name) < 0
	})

	context := struct {
		Entries     []colEntry
		PackageName string
	}{
		Entries:     entries,
		PackageName: packageName,
	}

	// Calculate the Go packages that needs to be imported for the proto types to be registered.
	return applyTemplate(staticCollectionsTemplate, context)
}
