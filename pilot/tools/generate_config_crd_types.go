// Copyright 2018 Istio Authors
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

// Tool to generate pilot/pkg/config/kube/types.go
// Example run command:
// go run pilot/tools/generate_config_crd_types.go --template pilot/tools/types.go.tmpl --output pilot/pkg/config/kube/crd/types.go
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"io/ioutil"
	"log"
	"text/template"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
)

// ConfigData is data struct to feed to types.go template.
type ConfigData struct {
	IstioKind string
	CrdKind   string
}

// MakeConfigData prepare data for code generation for the given schema.
func MakeConfigData(schema model.ProtoSchema) ConfigData {
	out := ConfigData{
		IstioKind: crd.KebabCaseToCamelCase(schema.Type),
		CrdKind:   crd.KebabCaseToCamelCase(schema.Type),
	}
	if len(schema.SchemaObjectName) > 0 {
		out.IstioKind = schema.SchemaObjectName
	}
	log.Printf("Generating Istio type %s for %s.%s CRD\n", out.IstioKind, out.CrdKind, schema.Group)
	return out
}

func main() {
	templateFile := flag.String("template", "pilot/tools/types.go.tmpl", "Template file")
	outputFile := flag.String("output", "", "Output file. Leave blank to go to stdout")
	flag.Parse()

	tmpl := template.Must(template.ParseFiles(*templateFile))

	// Prepare to generate types for mock schema and all schema listed in model.IstioConfigTypes
	typeList := []ConfigData{
		MakeConfigData(model.MockConfig),
	}
	for _, schema := range model.IstioConfigTypes {
		typeList = append(typeList, MakeConfigData(schema))
	}
	var buffer bytes.Buffer
	if err := tmpl.Execute(&buffer, typeList); err != nil {
		log.Fatal(err)
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
