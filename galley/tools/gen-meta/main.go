//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package main

import (
	"bytes"
	"fmt"
	"html/template"
	"io/ioutil"
	"os"
	"sort"
	"strings"

	"github.com/ghodss/yaml"
)

const usage = `

gen-meta [runtime|kube] <input-yaml-path> <output-go-path> 

`

var knownProtoTypes = map[string]struct{}{
	"google.protobuf.Struct": {},
}

// metadata is a combination of read and derived metadata.
type metadata struct {
	Items           []*entry   `json:"items"`
	ProtoGoPackages []string   `json:"-"`
	ProtoDefs       []protoDef `json:"-"`
}

// entry in a metadata file
type entry struct {
	Kind      string `json:"kind"`
	ListKind  string `json:"listKind"`
	Singular  string `json:"singular"`
	Plural    string `json:"plural"`
	Group     string `json:"group"`
	Version   string `json:"version"`
	Proto     string `json:"proto"`
	Gogo      bool   `json:"gogo"`
	Converter string `json:"converter"`
}

// proto related metadata
type protoDef struct {
	MessageName string `json:"-"`
	Gogo        bool   `json:"-"`
}

func main() {
	if len(os.Args) != 5 {
		fmt.Printf(usage)
		fmt.Printf("%v\n", os.Args)
		os.Exit(-1)
	}

	// The tool can generate both K8s level, as well as the proto level metadata.
	isRuntime := false
	switch os.Args[2] {
	case "kube":
		isRuntime = false
	case "runtime":
		isRuntime = true
	default:
		fmt.Printf("Unknown target: %v", os.Args[2])
		fmt.Printf(usage)
		os.Exit(-1)
	}

	input := os.Args[3]
	output := os.Args[4]

	m, err := readMetadata(input)
	if err != nil {
		fmt.Printf("%v", err)
		os.Exit(-2)
	}

	var contents []byte
	if isRuntime {
		contents, err = applyTemplate(runtimeTemplate, m)
	} else {
		contents, err = applyTemplate(kubeTemplate, m)
	}

	if err != nil {
		fmt.Printf("%v", err)
		os.Exit(-3)
	}

	if err = ioutil.WriteFile(output, contents, os.ModePerm); err != nil {
		fmt.Printf("%v", err)
		os.Exit(-4)
	}
}

func readMetadata(path string) (*metadata, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read input file: %v", err)
	}

	var m metadata

	if err = yaml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("error marshalling input file: %v", err)
	}

	// Auto-complete listkind fields with defaults.
	for _, item := range m.Items {
		if item.ListKind == "" {
			item.ListKind = item.Kind + "List"
		}
	}

	// Stable sort based on message name.
	sort.Slice(m.Items, func(i, j int) bool {
		return strings.Compare(m.Items[i].Proto, m.Items[j].Proto) < 0
	})

	// Calculate the Go packages that needs to be imported for the proto types to be registered.
	names := make(map[string]struct{})
	for _, e := range m.Items {
		if _, found := knownProtoTypes[e.Proto]; e.Proto == "" || found {
			continue
		}

		parts := strings.Split(e.Proto, ".")
		// Remove the first "istio", and the Proto itself
		parts = parts[1 : len(parts)-1]

		p := strings.Join(parts, "/")
		p = fmt.Sprintf("istio.io/api/%s", p)
		names[p] = struct{}{}
	}

	for k := range names {
		m.ProtoGoPackages = append(m.ProtoGoPackages, k)
	}
	sort.Strings(m.ProtoGoPackages)

	// Calculate the proto types that needs to be handled.
	// First, single instance the proto definitions.
	protoDefs := make(map[string]protoDef)
	for _, e := range m.Items {
		if _, found := knownProtoTypes[e.Proto]; e.Proto == "" || found {
			continue
		}
		defn := protoDef{MessageName: e.Proto, Gogo: e.Gogo}

		if prevDefn, ok := protoDefs[e.Proto]; ok && defn != prevDefn {
			return nil, fmt.Errorf("proto definitions do not match: %+v != %+v", defn, prevDefn)
		}
		protoDefs[e.Proto] = defn
	}

	for _, v := range protoDefs {
		m.ProtoDefs = append(m.ProtoDefs, v)
	}

	// Then, stable sort based on message name.
	sort.Slice(m.ProtoDefs, func(i, j int) bool {
		return strings.Compare(m.ProtoDefs[i].MessageName, m.ProtoDefs[j].MessageName) < 0
	})

	return &m, nil
}

const runtimeTemplate = `
// GENERATED FILE -- DO NOT EDIT
//
//go:generate $GOPATH/src/istio.io/istio/galley/tools/gen-meta/gen-meta.sh runtime pkg/runtime/resource/types.go
//

package resource

import (
// Pull in all the known proto types to ensure we get their types registered.
{{range .ProtoGoPackages}}	_ "{{.}}"
{{end}}
)

// Types of known resources.
var Types = NewSchema()

func init() {
{{range .ProtoDefs}}	Types.Register("type.googleapis.com/{{.MessageName}}", {{.Gogo}})
{{end}}}
`

const kubeTemplate = `
// GENERATED FILE -- DO NOT EDIT
//
//go:generate $GOPATH/src/istio.io/istio/galley/tools/gen-meta/gen-meta.sh kube pkg/kube/types.go
//

package kube

// Types in the schema.
var Types = Schema{}

func init() {
{{range .Items}}
	Types.add(ResourceSpec{
		Kind:       "{{.Kind}}",
		ListKind:   "{{.ListKind}}",
		Singular:   "{{.Singular}}",
		Plural:     "{{.Plural}}",
		Version:    "{{.Version}}",
		Group:      "{{.Group}}",
		Target:     getTargetFor("type.googleapis.com/{{.Proto}}"),
		Converter:  converter.Get("{{ if .Converter }}{{.Converter}}{{ else }}identity{{end}}"),
    })
{{end}}
}
`

func applyTemplate(tmpl string, m *metadata) ([]byte, error) {
	t := template.New("tmpl")

	t2, err := t.Parse(tmpl)
	if err != nil {
		return nil, err
	}

	var b bytes.Buffer
	if err = t2.Execute(&b, m); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}
