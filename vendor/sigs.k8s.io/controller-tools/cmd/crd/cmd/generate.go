/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cmd

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
	flag "github.com/spf13/pflag"
	crdgenerator "sigs.k8s.io/controller-tools/pkg/crd/generator"
)

var g *crdgenerator.Generator

// GenerateCmd represents the generate command
var GenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate CRD install files for the resource definition",
	Long: `Generate CRD install files for the resource definition.

- if param domain is specified, it will take given root-path as working directory,
and take current path if root-path not specified; note that api path "pkg/apis" must
exist under the root-path.
- if param domain is not specified, then it will search parent directories of root path
for PROJECT file, take its path as working directory, and fetch domain info from the file.
`,
	Example: "crd generate --domain k8s.io",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Writing CRD files...")
		if err := g.ValidateAndInitFields(); err != nil {
			log.Fatal(err)
		}
		if err := g.Do(); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("CRD files generated, files can be found under path %s.\n", g.OutputDir)
	},
}

func init() {
	rootCmd.AddCommand(GenerateCmd)
	g = GeneratorForFlags(GenerateCmd.Flags())
}

// GeneratorForFlags registers flags for Generator fields and returns the Generator.
func GeneratorForFlags(f *flag.FlagSet) *crdgenerator.Generator {
	g := &crdgenerator.Generator{}
	f.StringVar(&g.RootPath, "root-path", "", "working dir, must have PROJECT file under the path or parent path if domain not set")
	f.StringVar(&g.OutputDir, "output-dir", "", "output directory, default to 'config/crds' under root path")
	f.StringVar(&g.Domain, "domain", "", "domain of the resources, will try to fetch it from PROJECT file if not specified")
	// TODO: Do we need this? Is there a possibility that a crd is namespace scoped?
	f.StringVar(&g.Namespace, "namespace", "", "CRD namespace, treat it as root scoped if not set")
	f.BoolVar(&g.SkipMapValidation, "skip-map-validation", true, "if set to true, skip generating validation schema for map type in CRD.")
	return g
}
