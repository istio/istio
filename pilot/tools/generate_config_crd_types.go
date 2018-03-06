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

type ConfigData struct {
	IstioKind string
	CrdKind   string
}

func MakeConfigData(schema model.ProtoSchema) ConfigData {
	out := ConfigData{
		IstioKind: crd.KabobCaseToCamelCase(schema.Type),
		CrdKind:   crd.KabobCaseToCamelCase(schema.Type),
	}
	// Tweak to match current naming. This can be changed to meet the new naming convention.
	if schema.Group == "authenticaiton" {
		out.IstioKind = crd.KabobCaseToCamelCase(schema.Group + "-" + schema.Type)
	}
	log.Printf("Generating Istio type %s for %s.%s CRD\n", out.IstioKind, out.CrdKind, schema.Group)
	return out
}

func main() {
	templateFile := flag.String("template", "types.go.tmpl", "Template file")
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
	} else {
		if err := ioutil.WriteFile(*outputFile, out, 0644); err != nil {
			panic(err)
		}
	}
}
