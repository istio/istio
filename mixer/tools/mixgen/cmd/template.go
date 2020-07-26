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

package cmd

import (
	"bytes"
	"encoding/base64"
	"io/ioutil"
	"path/filepath"
	"strings"
	gotemplate "text/template"

	"github.com/spf13/cobra"

	"istio.io/istio/mixer/cmd/shared"
	"istio.io/istio/mixer/pkg/runtime/config/constant"
	"istio.io/istio/pkg/test/env"
)

func templateCfgCmd(rawArgs []string, printf, fatalf shared.FormatFn) *cobra.Command {
	var desc string
	var name string
	var output string
	var ns string
	adapterCmd := &cobra.Command{
		Use:   "template",
		Short: "creates kubernetes configuration for a template",
		Run: func(cmd *cobra.Command, args []string) {
			createTemplate("mixgen "+strings.Join(rawArgs, " "), name, ns, desc, output, fatalf, printf)
		},
	}
	adapterCmd.PersistentFlags().StringVarP(&desc, "descriptor", "d", "", "path to the template's "+
		"protobuf file descriptor set file (protobuf file descriptor set is created using "+
		"`protoc -o <path to template proto file> <Flags>`)")
	adapterCmd.PersistentFlags().StringVarP(&name, "name", "n", "", "name of the resource")
	adapterCmd.PersistentFlags().StringVarP(&output, "output", "o", "", "output file path"+
		" to save the configuration")
	adapterCmd.PersistentFlags().StringVar(&ns, "namespace", constant.DefaultConfigNamespace, "namespace of the resource")
	return adapterCmd
}

func createTemplate(rawCommand, name, ns, desc string, outPath string, fatalf shared.FormatFn, printf shared.FormatFn) {
	type templateCRVar struct {
		RawCommand string
		Name       string
		Namespace  string
		Descriptor string
	}
	const templateCR = `# this config is created through command
# {{.RawCommand}}
apiVersion: "config.istio.io/v1alpha2"
kind: template
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
spec:
  descriptor: "{{.Descriptor}}"
---
`

	inPath, _ := filepath.Abs(desc)
	byts, err := ioutil.ReadFile(inPath)
	if err != nil {
		fatalf("unable to read file %s. %v", inPath, err)
	}

	// validate if the file is a file descriptor set with imports.
	if err = isFds(byts); err != nil {
		fatalf("template in invalid: %v", err)
	}

	tmplObj := &templateCRVar{
		Name:       name,
		Namespace:  ns,
		Descriptor: base64.StdEncoding.EncodeToString(byts),
		RawCommand: strings.Replace(rawCommand, env.IstioSrc, "$REPO_ROOT", -1),
	}

	t := gotemplate.New("templatecr")
	w := &bytes.Buffer{}
	t, _ = t.Parse(templateCR)
	if err = t.Execute(w, tmplObj); err != nil {
		fatalf("could not create CRD " + err.Error())
	}
	if outPath != "" {
		if err = ioutil.WriteFile(outPath, w.Bytes(), 0644); err != nil {
			fatalf("cannot write to output file '%s': %v", outPath, err)
		}
	} else {
		printf(w.String())
	}
}
