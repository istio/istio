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

package generator

import (
	"fmt"
	"log"
	"os"
	"path"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/spf13/afero"
	extensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/gengo/args"
	"k8s.io/gengo/types"
	crdutil "sigs.k8s.io/controller-tools/pkg/crd/util"
	"sigs.k8s.io/controller-tools/pkg/internal/codegen"
	"sigs.k8s.io/controller-tools/pkg/internal/codegen/parse"
	"sigs.k8s.io/controller-tools/pkg/util"
)

// Generator generates CRD manifests from API resource definitions defined in Go source files.
type Generator struct {
	RootPath          string
	OutputDir         string
	Domain            string
	Namespace         string
	SkipMapValidation bool

	// OutFs is filesystem to be used for writing out the result
	OutFs afero.Fs

	// apisPkg is the absolute Go pkg name for current project's 'pkg/apis' pkg.
	// This is needed to determine if a Type belongs to the project or it is a referred Type.
	apisPkg string
}

// ValidateAndInitFields validate and init generator fields.
func (c *Generator) ValidateAndInitFields() error {
	var err error

	if c.OutFs == nil {
		c.OutFs = afero.NewOsFs()
	}

	if len(c.RootPath) == 0 {
		// Take current path as root path if not specified.
		c.RootPath, err = os.Getwd()
		if err != nil {
			return err
		}
	}

	// Validate root path is under go src path
	if !crdutil.IsUnderGoSrcPath(c.RootPath) {
		return fmt.Errorf("command must be run from path under $GOPATH/src/<package>")
	}

	// If Domain is not explicitly specified,
	// try to search for PROJECT file as a basis.
	if len(c.Domain) == 0 {
		if !crdutil.PathHasProjectFile(c.RootPath) {
			return fmt.Errorf("PROJECT file missing in dir %s", c.RootPath)
		}
		c.Domain = crdutil.GetDomainFromProject(c.RootPath)
	}

	// Validate apis directory exists under working path
	apisPath := path.Join(c.RootPath, "pkg/apis")
	if _, err := os.Stat(apisPath); err != nil {
		return fmt.Errorf("error validating apis path %s: %v", apisPath, err)
	}

	c.apisPkg, err = crdutil.DirToGoPkg(apisPath)
	if err != nil {
		return err
	}

	// Init output directory
	if c.OutputDir == "" {
		c.OutputDir = path.Join(c.RootPath, "config/crds")
	}

	return nil
}

// Do manages CRD generation.
func (c *Generator) Do() error {
	arguments := args.Default()
	b, err := arguments.NewBuilder()
	if err != nil {
		return fmt.Errorf("failed making a parser: %v", err)
	}

	// Switch working directory to root path.
	if err := os.Chdir(c.RootPath); err != nil {
		return fmt.Errorf("failed switching working dir: %v", err)
	}

	if err := b.AddDirRecursive("./pkg/apis"); err != nil {
		return fmt.Errorf("failed making a parser: %v", err)
	}
	ctx, err := parse.NewContext(b)
	if err != nil {
		return fmt.Errorf("failed making a context: %v", err)
	}

	arguments.CustomArgs = &parse.Options{SkipMapValidation: c.SkipMapValidation}

	// TODO: find an elegant way to fulfill the domain in APIs.
	p := parse.NewAPIs(ctx, arguments, c.Domain, c.apisPkg)
	crds := c.getCrds(p)

	return c.writeCRDs(crds)
}

func (c *Generator) writeCRDs(crds map[string][]byte) error {
	// Ensure output dir exists.
	if err := c.OutFs.MkdirAll(c.OutputDir, os.FileMode(0700)); err != nil {
		return err
	}

	for file, crd := range crds {
		outFile := path.Join(c.OutputDir, file)
		if err := (&util.FileWriter{Fs: c.OutFs}).WriteFile(outFile, crd); err != nil {
			return err
		}
	}
	return nil
}

func getCRDFileName(resource *codegen.APIResource) string {
	elems := []string{resource.Group, resource.Version, strings.ToLower(resource.Kind)}
	return strings.Join(elems, "_") + ".yaml"
}

func (c *Generator) getCrds(p *parse.APIs) map[string][]byte {
	crds := map[string]extensionsv1beta1.CustomResourceDefinition{}
	for _, g := range p.APIs.Groups {
		for _, v := range g.Versions {
			for _, r := range v.Resources {
				crd := r.CRD
				// ignore types which do not belong to this project
				if !c.belongsToAPIsPkg(r.Type) {
					continue
				}
				if len(c.Namespace) > 0 {
					crd.Namespace = c.Namespace
				}
				fileName := getCRDFileName(r)
				crds[fileName] = crd
			}
		}
	}

	result := map[string][]byte{}
	for file, crd := range crds {
		b, err := yaml.Marshal(crd)
		if err != nil {
			log.Fatalf("Error: %v", err)
		}
		result[file] = b
	}

	return result
}

// belongsToAPIsPkg returns true if type t is defined under pkg/apis pkg of
// current project.
func (c *Generator) belongsToAPIsPkg(t *types.Type) bool {
	return strings.HasPrefix(t.Name.Package, c.apisPkg)
}
