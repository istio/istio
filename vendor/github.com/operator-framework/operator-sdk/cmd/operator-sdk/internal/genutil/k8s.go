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
	"github.com/operator-framework/operator-sdk/internal/util/projutil"

	log "github.com/sirupsen/logrus"
)

// K8sCodegen performs deepcopy code-generation for all custom resources under
// pkg/apis. hf is  a path to a header file containing text to add to generated
// files.
func K8sCodegen(hf string) error {
	projutil.MustInProjectRoot()

	wd := projutil.MustGetwd()
	repoPkg := projutil.CheckAndGetProjectGoPkg()
	srcDir := filepath.Join(wd, "vendor", "k8s.io", "code-generator")
	binDir := filepath.Join(wd, scaffold.BuildBinDir)

	genDirs := []string{
		"./cmd/defaulter-gen",
		"./cmd/client-gen",
		"./cmd/lister-gen",
		"./cmd/informer-gen",
		"./cmd/deepcopy-gen",
	}
	if err := buildCodegenBinaries(genDirs, binDir, srcDir); err != nil {
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

	log.Infof("Running deepcopy code-generation for Custom Resource group versions: [%v]\n", gvb.String())

	fdc := func(a string) error { return deepcopyGen(binDir, repoPkg, a, gvMap) }
	if err = withHeaderFile(hf, fdc); err != nil {
		return err
	}
	fd := func(a string) error { return defaulterGen(binDir, repoPkg, a, gvMap) }
	if err = withHeaderFile(hf, fd); err != nil {
		return err
	}

	log.Info("Code-generation complete.")
	return nil
}

func deepcopyGen(binDir, repoPkg, hf string, gvMap map[string][]string) (err error) {
	apisPkg := filepath.Join(repoPkg, scaffold.ApisDir)
	args := []string{
		"--input-dirs", createFQApis(apisPkg, gvMap),
		"--output-file-base", "zz_generated.deepcopy",
		"--bounding-dirs", apisPkg,
		// deepcopy-gen requires a boilerplate file. Either use header or an
		// empty file if header is empty.
		"--go-header-file", hf,
	}
	cmd := exec.Command(filepath.Join(binDir, "deepcopy-gen"), args...)
	if err = projutil.ExecCmd(cmd); err != nil {
		return fmt.Errorf("failed to perform deepcopy code-generation: %v", err)
	}
	return nil
}

func defaulterGen(binDir, repoPkg, hf string, gvMap map[string][]string) (err error) {
	apisPkg := filepath.Join(repoPkg, scaffold.ApisDir)
	args := []string{
		"--input-dirs", createFQApis(apisPkg, gvMap),
		"--output-file-base", "zz_generated.defaults",
		// defaulter-gen requires a boilerplate file. Either use header or an
		// empty file if header is empty.
		"--go-header-file", hf,
	}
	cmd := exec.Command(filepath.Join(binDir, "defaulter-gen"), args...)
	if err = projutil.ExecCmd(cmd); err != nil {
		return fmt.Errorf("failed to perform defaulter code-generation: %v", err)
	}
	return nil
}
