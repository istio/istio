// Copyright 2018 The Operator-SDK Authors
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

package genutil

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold"
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
	"github.com/operator-framework/operator-sdk/internal/util/k8sutil"
	"github.com/operator-framework/operator-sdk/internal/util/projutil"

	log "github.com/sirupsen/logrus"
)

// OpenAPIGen generates OpenAPI validation specs for all CRD's in dirs. hf is
// a path to a header file containing text to add to generated files.
func OpenAPIGen(hf string) error {
	projutil.MustInProjectRoot()

	absProjectPath := projutil.MustGetwd()
	repoPkg := projutil.CheckAndGetProjectGoPkg()
	srcDir := filepath.Join(absProjectPath, "vendor", "k8s.io", "kube-openapi")
	binDir := filepath.Join(absProjectPath, scaffold.BuildBinDir)

	if err := buildOpenAPIGenBinary(binDir, srcDir); err != nil {
		return err
	}

	gvMap, err := parseGroupVersions()
	if err != nil {
		return fmt.Errorf("failed to parse group versions: (%v)", err)
	}
	gvb := &strings.Builder{}
	for g, vs := range gvMap {
		gvb.WriteString(fmt.Sprintf("%s:%v, ", g, vs))
	}

	log.Infof("Running OpenAPI code-generation for Custom Resource group versions: [%v]\n", gvb.String())

	apisPkg := filepath.Join(repoPkg, scaffold.ApisDir)
	fqApiStr := createFQApis(apisPkg, gvMap)
	fqApis := strings.Split(fqApiStr, ",")
	f := func(a string) error { return openAPIGen(binDir, a, fqApis) }
	if err = withHeaderFile(hf, f); err != nil {
		return err
	}

	s := &scaffold.Scaffold{}
	cfg := &input.Config{
		Repo:           repoPkg,
		AbsProjectPath: absProjectPath,
		ProjectName:    filepath.Base(absProjectPath),
	}
	crds, err := k8sutil.GetCRDs(scaffold.CRDsDir)
	if err != nil {
		return err
	}
	for _, crd := range crds {
		g, v, k := crd.Spec.Group, crd.Spec.Version, crd.Spec.Names.Kind
		if v == "" {
			if len(crd.Spec.Versions) != 0 {
				v = crd.Spec.Versions[0].Name
			} else {
				return fmt.Errorf("crd of group %s kind %s has no version", g, k)
			}
		}
		r, err := scaffold.NewResource(g+"/"+v, k)
		if err != nil {
			return err
		}
		err = s.Execute(cfg,
			&scaffold.CRD{Resource: r, IsOperatorGo: projutil.IsOperatorGo()},
		)
		if err != nil {
			return err
		}
	}

	log.Info("Code-generation complete.")
	return nil
}

func buildOpenAPIGenBinary(binDir, codegenSrcDir string) error {
	genDirs := []string{"./cmd/openapi-gen"}
	return buildCodegenBinaries(genDirs, binDir, codegenSrcDir)
}

func openAPIGen(binDir, hf string, fqApis []string) (err error) {
	cgPath := filepath.Join(binDir, "openapi-gen")
	for _, fqApi := range fqApis {
		args := []string{
			"--input-dirs", fqApi,
			"--output-package", fqApi,
			"--output-file-base", "zz_generated.openapi",
			// openapi-gen requires a boilerplate file. Either use header or an
			// empty file if header is empty.
			"--go-header-file", hf,
		}
		cmd := exec.Command(cgPath, args...)
		if err = projutil.ExecCmd(cmd); err != nil {
			return fmt.Errorf("failed to perform openapi code-generation: %v", err)
		}
	}
	return nil
}
