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

// Tool to generate pilot/pkg/config/kube/crdclient/types.gen.go
// Example run command:
// REPO_ROOT=`pwd` go generate ./pilot/pkg/config/kube/crdclient/...
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"io/ioutil"
	"log"
	"path"
	"text/template"

	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/test/env"
)

// ConfigData is data struct to feed to types.go template.
type ConfigData struct {
	Namespaced      bool
	VariableName    string
	APIImport       string
	ClientImport    string
	ClientGroupPath string
	ClientTypePath  string
	Kind            string

	// Support service-apis, which require a custom client and the Spec suffix
	Client     string
	TypeSuffix string
}

// MakeConfigData prepare data for code generation for the given schema.
func MakeConfigData(schema collection.Schema) ConfigData {
	out := ConfigData{
		Namespaced:      !schema.Resource().IsClusterScoped(),
		VariableName:    schema.VariableName(),
		APIImport:       apiImport[schema.Resource().ProtoPackage()],
		ClientImport:    clientGoImport[schema.Resource().ProtoPackage()],
		ClientGroupPath: clientGoAccessPath[schema.Resource().ProtoPackage()],
		ClientTypePath:  clientGoTypePath[schema.Resource().Plural()],
		Kind:            schema.Resource().Kind(),
		Client:          "ic",
	}
	if schema.Resource().Group() == "networking.x-k8s.io" {
		out.Client = "sc"
		out.TypeSuffix = "Spec"
	}
	log.Printf("Generating Istio type %s for %s/%s CRD\n", out.VariableName, out.APIImport, out.Kind)
	return out
}

var (
	// Mapping from istio/api path import to api import path
	apiImport = map[string]string{
		"istio.io/api/mixer/v1/config/client":    "mixerclientv1",
		"istio.io/api/networking/v1alpha3":       "networkingv1alpha3",
		"istio.io/api/security/v1beta1":          "securityv1beta1",
		"sigs.k8s.io/service-apis/apis/v1alpha1": "servicev1alpha1",
	}
	// Mapping from istio/api path import to client go import path
	clientGoImport = map[string]string{
		"istio.io/api/mixer/v1/config/client":    "clientconfigv1alpha3",
		"istio.io/api/networking/v1alpha3":       "clientnetworkingv1alpha3",
		"istio.io/api/security/v1beta1":          "clientsecurityv1beta1",
		"sigs.k8s.io/service-apis/apis/v1alpha1": "servicev1alpha1",
	}
	// Translates an api import path to the top level path in client-go
	clientGoAccessPath = map[string]string{
		"istio.io/api/mixer/v1/config/client":    "ConfigV1alpha2",
		"istio.io/api/networking/v1alpha3":       "NetworkingV1alpha3",
		"istio.io/api/security/v1beta1":          "SecurityV1beta1",
		"sigs.k8s.io/service-apis/apis/v1alpha1": "NetworkingV1alpha1",
	}
	// Translates a plural type name to the type path in client-go
	// TODO: can we automatically derive this? I don't think we can, its internal to the kubegen
	clientGoTypePath = map[string]string{
		"httpapispecbindings":    "HTTPAPISpecBindings",
		"httpapispecs":           "HTTPAPISpecs",
		"quotaspecbindings":      "QuotaSpecBindings",
		"quotaspecs":             "QuotaSpecs",
		"destinationrules":       "DestinationRules",
		"envoyfilters":           "EnvoyFilters",
		"gateways":               "Gateways",
		"serviceentries":         "ServiceEntries",
		"sidecars":               "Sidecars",
		"virtualservices":        "VirtualServices",
		"workloadentries":        "WorkloadEntries",
		"authorizationpolicies":  "AuthorizationPolicies",
		"peerauthentications":    "PeerAuthentications",
		"requestauthentications": "RequestAuthentications",
		"gatewayclasses":         "GatewayClasses",
		"httproutes":             "HTTPRoutes",
		"tcproutes":              "TcpRoutes",
		"trafficsplits":          "TrafficSplits",
	}
)

func main() {
	templateFile := flag.String("template", path.Join(env.IstioSrc, "pilot/pkg/config/kube/crdclient/gen/types.go.tmpl"), "Template file")
	outputFile := flag.String("output", "", "Output file. Leave blank to go to stdout")
	flag.Parse()

	tmpl := template.Must(template.ParseFiles(*templateFile))

	// Prepare to generate types for mock schema and all Istio schemas
	typeList := []ConfigData{}
	for _, s := range collections.PilotServiceApi.All() {
		typeList = append(typeList, MakeConfigData(s))
	}
	var buffer bytes.Buffer
	if err := tmpl.Execute(&buffer, typeList); err != nil {
		log.Fatal(fmt.Errorf("template: %v", err))
	}

	// Format source code.
	out, err := format.Source(buffer.Bytes())
	if err != nil {
		log.Fatal(err)
	}
	// Output
	if outputFile == nil || *outputFile == "" {
		fmt.Println(string(out))
	} else if err := ioutil.WriteFile(*outputFile, out, 0644); err != nil {
		panic(err)
	}
}
