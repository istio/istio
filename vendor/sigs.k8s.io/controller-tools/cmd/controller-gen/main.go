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
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	crdgenerator "sigs.k8s.io/controller-tools/pkg/crd/generator"
	"sigs.k8s.io/controller-tools/pkg/rbac"
	"sigs.k8s.io/controller-tools/pkg/webhook"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "controller-gen",
		Short: "A reference implementation generation tool for Kubernetes APIs.",
		Long:  `A reference implementation generation tool for Kubernetes APIs.`,
		Example: `	# Generate RBAC manifests for a project
	controller-gen rbac
	
	# Generate CRD manifests for a project
	controller-gen crd 

	# Run all the generators for a given project
	controller-gen all
`,
	}

	rootCmd.AddCommand(
		newRBACCmd(),
		newCRDCmd(),
		newWebhookCmd(),
		newAllSubCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func newRBACCmd() *cobra.Command {
	o := &rbac.ManifestOptions{}
	o.SetDefaults()

	cmd := &cobra.Command{
		Use:   "rbac",
		Short: "Generates RBAC manifests",
		Long: `Generate RBAC manifests from the RBAC annotations in Go source files.
Usage:
# controller-gen rbac [--name manager] [--input-dir input_dir] [--output-dir output_dir]
`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := rbac.Generate(o); err != nil {
				log.Fatal(err)
			}
			fmt.Printf("RBAC manifests generated under '%s' directory\n", o.OutputDir)
		},
	}

	f := cmd.Flags()
	f.StringVar(&o.Name, "name", o.Name, "Name to be used as prefix in identifier for manifests")
	f.StringVar(&o.InputDir, "input-dir", o.InputDir, "input directory pointing to Go source files")
	f.StringVar(&o.OutputDir, "output-dir", o.OutputDir, "output directory where generated manifests will be saved.")

	return cmd
}

func newCRDCmd() *cobra.Command {
	g := &crdgenerator.Generator{}

	cmd := &cobra.Command{
		Use:   "crd",
		Short: "Generates CRD manifests",
		Long: `Generate CRD manifests from the Type definitions in Go source files.
Usage:
# controller-gen crd [--domain k8s.io] [--root-path input_dir] [--output-dir output_dir]
`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := g.ValidateAndInitFields(); err != nil {
				log.Fatal(err)
			}
			if err := g.Do(); err != nil {
				log.Fatal(err)
			}
			fmt.Printf("CRD files generated, files can be found under path %s.\n", g.OutputDir)
		},
	}

	f := cmd.Flags()
	f.StringVar(&g.RootPath, "root-path", "", "working dir, must have PROJECT file under the path or parent path if domain not set")
	f.StringVar(&g.OutputDir, "output-dir", "", "output directory, default to 'config/crds' under root path")
	f.StringVar(&g.Domain, "domain", "", "domain of the resources, will try to fetch it from PROJECT file if not specified")
	f.StringVar(&g.Namespace, "namespace", "", "CRD namespace, treat it as cluster scoped if not set")
	f.BoolVar(&g.SkipMapValidation, "skip-map-validation", true, "if set to true, skip generating OpenAPI validation schema for map type in CRD.")

	return cmd
}

func newAllSubCmd() *cobra.Command {
	var (
		projectDir, namespace string
	)

	cmd := &cobra.Command{
		Use:   "all",
		Short: "runs all generators for a project",
		Long: `Run all available generators for a given project
Usage:
# controller-gen all
`,
		Run: func(cmd *cobra.Command, args []string) {
			if projectDir == "" {
				currDir, err := os.Getwd()
				if err != nil {
					log.Fatalf("project-dir missing, failed to use current directory: %v", err)
				}
				projectDir = currDir
			}
			crdGen := &crdgenerator.Generator{
				RootPath:          projectDir,
				OutputDir:         filepath.Join(projectDir, "config", "crds"),
				Namespace:         namespace,
				SkipMapValidation: true,
			}
			if err := crdGen.ValidateAndInitFields(); err != nil {
				log.Fatal(err)
			}
			if err := crdGen.Do(); err != nil {
				log.Fatal(err)
			}
			fmt.Printf("CRD manifests generated under '%s' \n", crdGen.OutputDir)

			// RBAC generation
			rbacOptions := &rbac.ManifestOptions{
				InputDir:  filepath.Join(projectDir, "pkg"),
				OutputDir: filepath.Join(projectDir, "config", "rbac"),
				Name:      "manager",
			}
			if err := rbac.Generate(rbacOptions); err != nil {
				log.Fatal(err)
			}
			fmt.Printf("RBAC manifests generated under '%s' \n", rbacOptions.OutputDir)
		},
	}
	f := cmd.Flags()
	f.StringVar(&projectDir, "project-dir", "", "project directory, it must have PROJECT file")
	f.StringVar(&namespace, "namespace", "", "CRD namespace, treat it as cluster scoped if not set")
	return cmd
}

func newWebhookCmd() *cobra.Command {
	o := &webhook.ManifestOptions{}
	o.SetDefaults()

	cmd := &cobra.Command{
		Use:   "webhook",
		Short: "Generates webhook related manifests",
		Long: `Generate webhook related manifests from the webhook annotations in Go source files.
Usage:
# controller-gen webhook [--input-dir input_dir] [--output-dir output_dir] [--patch-output-dir patch-output_dir]
`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := webhook.Generate(o); err != nil {
				log.Fatal(err)
			}
			fmt.Printf("webhook manifests generated under '%s' directory\n", o.OutputDir)
		},
	}

	f := cmd.Flags()
	f.StringVar(&o.InputDir, "input-dir", o.InputDir, "input directory pointing to Go source files")
	f.StringVar(&o.OutputDir, "output-dir", o.OutputDir, "output directory where generated manifests will be saved.")
	f.StringVar(&o.PatchOutputDir, "patch-output-dir", o.PatchOutputDir, "output directory where generated kustomize patch will be saved.")

	return cmd
}
