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
	"os"
	"path/filepath"
	"sort"
	"strings"

	"istio.io/istio/pkg/config/schema/ast"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/util/sets"
)

const gvkTemplate = `
// GENERATED FILE -- DO NOT EDIT
//

package {{.PackageName}}

import (
	"istio.io/istio/pkg/config"
)

var (
{{- range .Entries }}
	{{.Resource.Identifier}} = config.GroupVersionKind{Group: "{{.Resource.Group}}", Version: "{{.Resource.Version}}", Kind: "{{.Resource.Kind}}"}
{{- end }}
)
`

const kindTemplate = `
// GENERATED FILE -- DO NOT EDIT
//

package {{.PackageName}}

import (
	"istio.io/istio/pkg/config"
)

const (
{{- range $index, $element := .Entries }}
	{{- if (eq $index 0) }}
	{{.Resource.Identifier}} Kind = iota
	{{- else }}
	{{.Resource.Identifier}}
	{{- end }}
{{- end }}
)

func (k Kind) String() string {
	switch k {
{{- range .Entries }}
	case {{.Resource.Identifier}}:
		return "{{.Resource.Kind}}"
{{- end }}
	default:
		return "Unknown"
	}
}

func FromGvk(gvk config.GroupVersionKind) Kind {
{{- range .Entries }}
	if gvk.Kind == "{{.Resource.Kind}}" && gvk.Group == "{{.Resource.Group}}" && gvk.Version == "{{.Resource.Version}}" {
		return {{.Resource.Identifier}}
	}
{{- end }}

	panic("unknown kind: " + gvk.String())
}
`

const collectionsTemplate = `
{{- .FilePrefix}}
// GENERATED FILE -- DO NOT EDIT
//

package {{.PackageName}}

import (
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/config/validation"
  "reflect"
{{- range .Packages}}
	{{.ImportName}} "{{.PackageName}}"
{{- end}}
)

var (
{{ range .Entries }}
	{{ .Resource.Identifier }} = resource.Builder {
			Identifier: "{{ .Resource.Identifier }}",
			Group: "{{ .Resource.Group }}",
			Kind: "{{ .Resource.Kind }}",
			Plural: "{{ .Resource.Plural }}",
			Version: "{{ .Resource.Version }}",
			{{- if .Resource.VersionAliases }}
            VersionAliases: []string{
				{{- range $alias := .Resource.VersionAliases}}
			        "{{$alias}}",
		 	    {{- end}}
			},
			{{- end}}
			Proto: "{{ .Resource.Proto }}",
			{{- if ne .Resource.StatusProto "" }}StatusProto: "{{ .Resource.StatusProto }}",{{end}}
			ReflectType: {{ .Type }},
			{{- if ne .StatusType "" }}StatusType: {{ .StatusType }}, {{end}}
			ProtoPackage: "{{ .Resource.ProtoPackage }}",
			{{- if ne "" .Resource.StatusProtoPackage}}StatusPackage: "{{ .Resource.StatusProtoPackage }}", {{end}}
			ClusterScoped: {{ .Resource.ClusterScoped }},
			Builtin: {{ .Resource.Builtin }},
			ValidateProto: validation.{{ .Resource.Validate }},
		}.MustBuild()
{{ end }}

	// All contains all collections in the system.
	All = collection.NewSchemasBuilder().
	{{- range .Entries }}
		MustAdd({{ .Resource.Identifier }}).
	{{- end }}
		Build()

	// Kube contains only kubernetes collections.
	Kube = collection.NewSchemasBuilder().
	{{- range .Entries }}
		{{- if or (contains .Resource.Group "k8s.io") .Resource.Builtin  }}
		MustAdd({{ .Resource.Identifier }}).
		{{- end }}
	{{- end }}
		Build()

	// Pilot contains only collections used by Pilot.
	Pilot = collection.NewSchemasBuilder().
	{{- range .Entries }}
		{{- if (contains .Resource.Group "istio.io") }}
		MustAdd({{ .Resource.Identifier }}).
		{{- end}}
	{{- end }}
		Build()

	// PilotGatewayAPI contains only collections used by Pilot, including experimental Service Api.
	PilotGatewayAPI = collection.NewSchemasBuilder().
	{{- range .Entries }}
		{{- if or (contains .Resource.Group "istio.io") (contains .Resource.Group "gateway.networking.k8s.io") }}
		MustAdd({{ .Resource.Identifier }}).
		{{- end}}
	{{- end }}
		Build()
)
`

type colEntry struct {
	Resource   *ast.Resource
	Type       string
	StatusType string
}

type inputs struct {
	Entries  []colEntry
	Packages []packageImport
}

func buildInputs() (inputs, error) {
	b, err := os.ReadFile(filepath.Join(env.IstioSrc, "pkg/config/schema/metadata.yaml"))
	if err != nil {
		fmt.Printf("unable to read input file: %v", err)
		return inputs{}, err
	}

	// Parse the file.
	m, err := ast.Parse(string(b))
	if err != nil {
		fmt.Printf("failed parsing input file: %v", err)
		return inputs{}, err
	}
	entries := make([]colEntry, 0, len(m.Resources))
	for _, r := range m.Resources {
		spl := strings.Split(r.Proto, ".")
		tname := spl[len(spl)-1]
		stat := strings.Split(r.StatusProto, ".")
		statName := stat[len(stat)-1]
		e := colEntry{
			Resource: r,
			Type:     fmt.Sprintf("reflect.TypeOf(&%s.%s{}).Elem()", toImport(r.ProtoPackage), tname),
		}
		if r.StatusProtoPackage != "" {
			e.StatusType = fmt.Sprintf("reflect.TypeOf(&%s.%s{}).Elem()", toImport(r.StatusProtoPackage), statName)
		}
		entries = append(entries, e)
	}

	sort.Slice(entries, func(i, j int) bool {
		return strings.Compare(entries[i].Resource.Identifier, entries[j].Resource.Identifier) < 0
	})

	// Single instance and sort names
	names := sets.New[string]()

	for _, r := range m.Resources {
		if r.ProtoPackage != "" {
			names.Insert(r.ProtoPackage)
		}
		if r.StatusProtoPackage != "" {
			names.Insert(r.StatusProtoPackage)
		}
	}

	packages := make([]packageImport, 0, names.Len())
	for p := range names {
		packages = append(packages, packageImport{p, toImport(p)})
	}
	sort.Slice(packages, func(i, j int) bool {
		return strings.Compare(packages[i].PackageName, packages[j].PackageName) < 0
	})

	return inputs{
		Entries:  entries,
		Packages: packages,
	}, nil
}

type packageImport struct {
	PackageName string
	ImportName  string
}

func toImport(p string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(p, "/", ""), ".", ""), "-", "")
}
