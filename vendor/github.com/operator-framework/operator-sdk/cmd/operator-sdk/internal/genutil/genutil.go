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
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold"
	"github.com/operator-framework/operator-sdk/internal/util/projutil"

	log "github.com/sirupsen/logrus"
)

func buildCodegenBinaries(genDirs []string, binDir, codegenSrcDir string) error {
	for _, gd := range genDirs {
		err := runGoBuildCodegen(binDir, codegenSrcDir, gd)
		if err != nil {
			return err
		}
	}
	return nil
}

func runGoBuildCodegen(binDir, repoDir, genDir string) error {
	binPath := filepath.Join(binDir, filepath.Base(genDir))
	cmd := exec.Command("go", "build", "-o", binPath, genDir)
	cmd.Dir = repoDir
	if gf, ok := os.LookupEnv(projutil.GoFlagsEnv); ok && len(gf) != 0 {
		cmd.Env = append(os.Environ(), projutil.GoFlagsEnv+"="+gf)
	}

	// Only print binary build info if verbosity is explicitly set.
	if projutil.IsGoVerbose() {
		return projutil.ExecCmd(cmd)
	}
	cmd.Stdout = ioutil.Discard
	cmd.Stderr = ioutil.Discard
	return cmd.Run()
}

// ParseGroupVersions parses the layout of pkg/apis to return a map of
// API groups to versions.
func parseGroupVersions() (map[string][]string, error) {
	gvs := make(map[string][]string)
	groups, err := ioutil.ReadDir(scaffold.ApisDir)
	if err != nil {
		return nil, fmt.Errorf("could not read pkg/apis directory to find api Versions: %v", err)
	}

	for _, g := range groups {
		if g.IsDir() {
			groupDir := filepath.Join(scaffold.ApisDir, g.Name())
			versions, err := ioutil.ReadDir(groupDir)
			if err != nil {
				return nil, fmt.Errorf("could not read %s directory to find api Versions: %v", groupDir, err)
			}

			gvs[g.Name()] = make([]string, 0)
			for _, v := range versions {
				if v.IsDir() && scaffold.ResourceVersionRegexp.MatchString(v.Name()) {
					gvs[g.Name()] = append(gvs[g.Name()], v.Name())
				}
			}
		}
	}

	if len(gvs) == 0 {
		return nil, fmt.Errorf("no groups or versions found in %s", scaffold.ApisDir)
	}
	return gvs, nil
}

// CreateFQApis return a string of all fully qualified pkg + groups + versions
// of pkg and gvs in the format:
// "pkg/groupA/v1,pkg/groupA/v2,pkg/groupB/v1"
func createFQApis(pkg string, gvs map[string][]string) string {
	gn := 0
	fqb := &strings.Builder{}
	for g, vs := range gvs {
		for vn, v := range vs {
			fqb.WriteString(filepath.Join(pkg, g, v))
			if vn < len(vs)-1 {
				fqb.WriteString(",")
			}
		}
		if gn < len(gvs)-1 {
			fqb.WriteString(",")
		}
		gn++
	}
	return fqb.String()
}

func withHeaderFile(hf string, f func(string) error) (err error) {
	if hf == "" {
		hf, err = createEmptyTmpFile()
		if err != nil {
			return err
		}
		defer func() {
			if err = os.RemoveAll(hf); err != nil {
				log.Error(err)
			}
		}()
	}
	return f(hf)
}

func createEmptyTmpFile() (string, error) {
	f, err := ioutil.TempFile("", "")
	if err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}
