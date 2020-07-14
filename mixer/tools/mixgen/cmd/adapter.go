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
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	gotemplate "text/template"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	"github.com/spf13/cobra"

	"istio.io/istio/mixer/cmd/shared"
	"istio.io/istio/mixer/pkg/runtime/config/constant"
	"istio.io/istio/pkg/test/env"
)

func adapterCfgCmd(rawArgs []string, printf, fatalf shared.FormatFn) *cobra.Command {
	var resName string
	var ns string
	var configFilePath string
	var output string
	var description string
	var sessionBased bool
	var templates []string

	adapterCmd := &cobra.Command{
		Use:   "adapter",
		Short: "creates kubernetes configuration for an adapter",
		Run: func(cmd *cobra.Command, args []string) {
			createAdapterCr("mixgen "+strings.Join(rawArgs, " "), resName, ns, description, configFilePath,
				sessionBased, templates, output, printf, fatalf)
		},
	}
	adapterCmd.PersistentFlags().StringVarP(&resName, "name", "n", "", "name of the resource")
	adapterCmd.PersistentFlags().StringVar(&ns, "namespace", constant.DefaultConfigNamespace, "namespace of the resource")
	adapterCmd.PersistentFlags().StringVarP(&description, "description", "d", "",
		"description of the adapter")
	adapterCmd.PersistentFlags().StringVarP(&configFilePath, "config", "c", "",
		"path to the adapter config's protobuf file descriptor set file "+
			"(protobuf file descriptor set is created using `protoc -o <path to adapter config proto file> <Flags>`)")
	adapterCmd.PersistentFlags().BoolVarP(&sessionBased, "session_based", "s", true,
		"whether the adapter is session based or not. TODO link to the documentation")
	adapterCmd.PersistentFlags().StringArrayVarP(&templates, "templates", "t", nil,
		"supported template names")
	adapterCmd.PersistentFlags().StringVarP(&output, "output", "o", "", "output file path"+
		" to save the configuration; if it a directory the generate file name is <dir name>/<adapter name>.yaml")
	return adapterCmd
}

func createAdapterCr(rawCommand string, name, namespace, description, config string, sessionBased bool, templates []string,
	outPath string, printf, fatalf shared.FormatFn) {
	type adapterCRVar struct {
		RawCommand   string
		Name         string
		Namespace    string
		Description  string
		Config       string
		SessionBased bool
		Templates    []string
	}
	adapterTmpl := `# this config is created through command
# {{.RawCommand}}
apiVersion: "config.istio.io/v1alpha2"
kind: adapter
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
spec:
  description: {{.Description}}
  session_based: {{.SessionBased}}
  templates:
  {{range .Templates -}}
  - {{.}}
  {{end -}}
  config: {{.Config}}
---
`

	var byts []byte
	var err error

	if config != "" {
		// no config means adapter has no config.
		inPath, _ := filepath.Abs(config)
		byts, err = ioutil.ReadFile(inPath)
		if err != nil {
			fatalf("unable to read file %s. %v", inPath, err)
		}
		// validate if the file is a file descriptor set with imports.
		if err = isFds(byts); err != nil {
			fatalf("config in invalid: %v", err)
		}
	}

	adapterObj := &adapterCRVar{
		RawCommand:   strings.Replace(rawCommand, env.IstioSrc, "$REPO_ROOT", -1),
		Name:         name,
		Namespace:    namespace,
		Description:  description,
		Config:       base64.StdEncoding.EncodeToString(byts),
		SessionBased: sessionBased,
		Templates:    templates,
	}

	t := gotemplate.New("adaptercr")
	w := &bytes.Buffer{}
	t, _ = t.Parse(adapterTmpl)
	if err = t.Execute(w, adapterObj); err != nil {
		fatalf("could not create adapter custom resource" + err.Error())
	}

	if outPath != "" {
		var s os.FileInfo
		if s, err = os.Stat(outPath); err != nil {
			fatalf("cannot write to output file '%s': %v", outPath, err)
		}

		if s.IsDir() {
			outPath = path.Join(outPath, adapterObj.Name+".yaml")
		}

		if err = ioutil.WriteFile(outPath, w.Bytes(), 0644); err != nil {
			fatalf("cannot write to output file '%s': %v", outPath, err)
		}
	} else {
		printf(w.String())
	}
}

func isFds(byts []byte) error {
	fds := &descriptor.FileDescriptorSet{}
	err := proto.Unmarshal(byts, fds)
	if err != nil {
		return err
	}

	if len(fds.File) == 1 && len(fds.File[0].Dependency) > 0 {
		// fds is created without --include_imports.
		return fmt.Errorf("the file descriptor set was created without including imports" +
			". Please run protoc with `--include_imports` flag")
	}
	return nil
}
