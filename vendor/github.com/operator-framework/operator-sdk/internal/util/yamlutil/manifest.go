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

package yamlutil

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold"
	"github.com/operator-framework/operator-sdk/internal/util/fileutil"

	log "github.com/sirupsen/logrus"
)

var yamlSep = []byte("\n\n---\n\n")

// CombineManifests combines given manifests with a base manifest and adds yaml
// style separation. Nothing is appended if the manifest is empty or base
// already contains a trailing separator.
func CombineManifests(base []byte, manifests ...[]byte) []byte {
	// Base already has manifests we're appending to.
	base = bytes.Trim(base, " \n")
	if len(base) > 0 && len(manifests) > 0 {
		if i := bytes.LastIndex(base, bytes.Trim(yamlSep, "\n")); i != len(base)-3 {
			base = append(base, yamlSep...)
		}
	}
	for j, manifest := range manifests {
		if len(manifest) > 0 {
			base = append(base, bytes.Trim(manifest, " \n")...)
			// Don't append sep if manifest is the last element in manifests.
			if j < len(manifests)-1 {
				base = append(base, yamlSep...)
			}
		}
	}
	return append(base, '\n')
}

// GenerateCombinedNamespacedManifest creates a temporary manifest yaml
// by combining all standard namespaced resource manifests in deployDir.
func GenerateCombinedNamespacedManifest(deployDir string) (*os.File, error) {
	file, err := ioutil.TempFile("", "namespaced-manifest.yaml")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil && !fileutil.IsClosedError(err) {
			log.Errorf("Failed to close file %s: (%v)", file.Name(), err)
		}
	}()

	sa, err := ioutil.ReadFile(filepath.Join(deployDir, scaffold.ServiceAccountYamlFile))
	if err != nil {
		log.Warnf("Could not find the serviceaccount manifest: (%v)", err)
	}
	role, err := ioutil.ReadFile(filepath.Join(deployDir, scaffold.RoleYamlFile))
	if err != nil {
		log.Warnf("Could not find role manifest: (%v)", err)
	}
	roleBinding, err := ioutil.ReadFile(filepath.Join(deployDir, scaffold.RoleBindingYamlFile))
	if err != nil {
		log.Warnf("Could not find role_binding manifest: (%v)", err)
	}
	operator, err := ioutil.ReadFile(filepath.Join(deployDir, scaffold.OperatorYamlFile))
	if err != nil {
		return nil, fmt.Errorf("could not find operator manifest: (%v)", err)
	}
	combined := []byte{}
	combined = CombineManifests(combined, sa, role, roleBinding, operator)

	if err := file.Chmod(os.FileMode(fileutil.DefaultFileMode)); err != nil {
		return nil, fmt.Errorf("could not chown temporary namespaced manifest file: (%v)", err)
	}
	if _, err := file.Write(combined); err != nil {
		return nil, fmt.Errorf("could not create temporary namespaced manifest file: (%v)", err)
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return file, nil
}

// GenerateCombinedGlobalManifest creates a temporary manifest yaml
// by combining all standard global resource manifests in crdsDir.
func GenerateCombinedGlobalManifest(crdsDir string) (*os.File, error) {
	file, err := ioutil.TempFile("", "global-manifest.yaml")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil && !fileutil.IsClosedError(err) {
			log.Errorf("Failed to close file %s: (%v)", file.Name(), err)
		}
	}()

	files, err := ioutil.ReadDir(crdsDir)
	if err != nil {
		return nil, fmt.Errorf("could not read deploy directory: (%v)", err)
	}
	combined := []byte{}
	for _, file := range files {
		if strings.HasSuffix(file.Name(), "crd.yaml") {
			fileBytes, err := ioutil.ReadFile(filepath.Join(crdsDir, file.Name()))
			if err != nil {
				return nil, fmt.Errorf("could not read file %s: (%v)", filepath.Join(crdsDir, file.Name()), err)
			}
			combined = CombineManifests(combined, fileBytes)
		}
	}

	if err := file.Chmod(os.FileMode(fileutil.DefaultFileMode)); err != nil {
		return nil, fmt.Errorf("could not chown temporary global manifest file: (%v)", err)
	}
	if _, err := file.Write(combined); err != nil {
		return nil, fmt.Errorf("could not create temporary global manifest file: (%v)", err)
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return file, nil
}
