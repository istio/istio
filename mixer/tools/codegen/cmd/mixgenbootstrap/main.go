// Copyright 2017 Istio Authors
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

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"istio.io/mixer/tools/codegen/pkg/bootstrapgen"
)

func withArgs(args []string, errorf func(format string, a ...interface{})) {

	var outFilePath string
	var mappings []string

	// TODO add support for passing import mapping for individual template. For now, this tool just takes
	// single import mapping which applies to all the passed in templates.

	rootCmd := cobra.Command{
		Use: "mixgenbootstrap <list of file descriptor set protobufs>",
		Short: "Parses all the [Templates](http://TODO), defined in each of the input file descriptor set, and generates" +
			" the required Go file that mixer framework will be compiled with to add support for the pass in templates.",
		Long: "Each of the input <File descriptor set protobuf> must contain a proto file that defines the template.\n" +
			"This code generator is meant to be used for creating a new mixer build. The output file suppose to be integrated \n" +
			"in the mixer's build system.\n" +
			"Example: mixgenbootstrap metricTemplateFileDescriptorSet.pb quotaTemplateFileDescriptorSet.pb quotaTemplateFileDescriptorSet.pb -o template.gen.go",
		Run: func(cmd *cobra.Command, args []string) {

			if len(args) <= 0 {
				errorf("Must specify at least one file descriptor set protobuf file.")
			}

			outFileFullPath, err := filepath.Abs(outFilePath)
			if err != nil {
				errorf("Invalid path %s. %v", outFilePath, err)
			}
			importMapping := make(map[string]string)
			for _, maps := range mappings {
				m := strings.Split(maps, ":")
				importMapping[strings.TrimSpace(m[0])] = strings.TrimSpace(m[1])
			}

			generator := bootstrapgen.Generator{OutFilePath: outFileFullPath, ImportMapping: importMapping}
			if err := generator.Generate(args); err != nil {
				errorf("%v", err)
			}
		},
	}

	rootCmd.SetArgs(args)

	rootCmd.PersistentFlags().StringVarP(&outFilePath, "output", "o", "./template.gen.go", "Output "+
		"path for generated Go source file.")

	rootCmd.PersistentFlags().StringArrayVarP(&mappings, "importmapping",
		"m", []string{},
		"colon separated mapping of proto import to Go package names."+
			" -m google/protobuf/descriptor.proto:github.com/golang/protobuf/protoc-gen-go/descriptor")

	if err := rootCmd.Execute(); err != nil {
		errorf("%v", err)
	}
}

func main() {
	withArgs(os.Args[1:],
		func(format string, a ...interface{}) {
			fmt.Fprintf(os.Stderr, format+"\n", a...)
			os.Exit(1)
		})
}
