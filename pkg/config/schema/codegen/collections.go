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
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stoewer/go-strcase"

	"istio.io/istio/pkg/config/schema/ast"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/util/sets"
)

//go:embed templates/gvk.go.tmpl
var gvkTemplate string

//go:embed templates/gvr.go.tmpl
var gvrTemplate string

//go:embed templates/crdclient.go.tmpl
var crdclientTemplate string

//go:embed templates/types.go.tmpl
var typesTemplate string

//go:embed templates/clients.go.tmpl
var clientsTemplate string

//go:embed templates/kind.go.tmpl
var kindTemplate string

//go:embed templates/collections.go.tmpl
var collectionsTemplate string

type colEntry struct {
	Resource *ast.Resource

	// ClientImport represents the import alias for the client. Example: clientnetworkingv1alpha3.
	ClientImport string
	// ClientImport represents the import alias for the status. Example: clientnetworkingv1alpha3.
	StatusImport string
	// IstioAwareClientImport represents the import alias for the API, taking into account Istio storing its API (spec)
	// separate from its client import
	// Example: apiclientnetworkingv1alpha3.
	IstioAwareClientImport string
	// ClientGroupPath represents the group in the client. Example: NetworkingV1alpha3.
	ClientGroupPath string
	// ClientGetter returns the path to get the client from a kube.Client. Example: Istio.
	ClientGetter string
	// ClientTypePath returns the kind name. Basically upper cased "plural". Example: Gateways
	ClientTypePath string
	// SpecType returns the type of the Spec field. Example: HTTPRouteSpec.
	SpecType   string
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
			Resource:               r,
			ClientImport:           toImport(r.ProtoPackage),
			StatusImport:           toImport(r.StatusProtoPackage),
			IstioAwareClientImport: toIstioAwareImport(r.ProtoPackage, r.Version),
			ClientGroupPath:        toGroup(r.ProtoPackage, r.Version),
			ClientGetter:           toGetter(r.ProtoPackage),
			ClientTypePath:         toTypePath(r),
			SpecType:               tname,
		}
		if r.StatusProtoPackage != "" {
			e.StatusType = statName
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

func toTypePath(r *ast.Resource) string {
	k := r.Kind
	g := r.Plural
	res := strings.Builder{}
	for i, c := range g {
		if i >= len(k) {
			res.WriteByte(byte(c))
		} else {
			if k[i] == bytes.ToUpper([]byte{byte(c)})[0] {
				res.WriteByte(k[i])
			} else {
				res.WriteByte(byte(c))
			}
		}
	}
	return res.String()
}

func toGetter(protoPackage string) string {
	if strings.Contains(protoPackage, "istio.io") {
		return "Istio"
	} else if strings.Contains(protoPackage, "sigs.k8s.io/gateway-api-inference-extension") {
		return "GatewayAPIInference"
	} else if strings.Contains(protoPackage, "sigs.k8s.io/gateway-api") {
		return "GatewayAPI"
	} else if strings.Contains(protoPackage, "k8s.io/apiextensions-apiserver") {
		return "Ext"
	}
	return "Kube"
}

func toGroup(protoPackage string, version string) string {
	p := strings.Split(protoPackage, "/")
	e := len(p) - 1
	if strings.Contains(protoPackage, "sigs.k8s.io/gateway-api/apisx") {
		// Custom naming for "X" types
		return "Experimental" + strcase.UpperCamelCase(version)
	} else if strings.Contains(protoPackage, "sigs.k8s.io/gateway-api-inference-extension") {
		// Gateway has one level of nesting with custom name
		return "Inference" + strcase.UpperCamelCase(version)
	} else if strings.Contains(protoPackage, "sigs.k8s.io/gateway-api") {
		// Gateway has one level of nesting with custom name
		return "Gateway" + strcase.UpperCamelCase(version)
	}
	// rest have two levels of nesting
	return strcase.UpperCamelCase(p[e-1]) + strcase.UpperCamelCase(version)
}

type packageImport struct {
	PackageName string
	ImportName  string
}

func toImport(p string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(p, "/", ""), ".", ""), "-", "")
}

func toIstioAwareImport(protoPackage string, version string) string {
	p := strings.Split(protoPackage, "/")
	base := strings.Join(p[:len(p)-1], "")
	imp := strings.ReplaceAll(strings.ReplaceAll(base, ".", ""), "-", "") + version
	if strings.Contains(protoPackage, "istio.io") {
		return "api" + imp
	}
	return imp
}
