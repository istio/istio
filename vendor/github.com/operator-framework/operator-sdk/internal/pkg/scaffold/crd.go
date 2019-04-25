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

package scaffold

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
	"github.com/operator-framework/operator-sdk/internal/util/k8sutil"

	"github.com/ghodss/yaml"
	"github.com/spf13/afero"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crdgenerator "sigs.k8s.io/controller-tools/pkg/crd/generator"
)

// CRD is the input needed to generate a deploy/crds/<group>_<version>_<kind>_crd.yaml file
type CRD struct {
	input.Input

	// Resource defines the inputs for the new custom resource definition
	Resource *Resource

	// IsOperatorGo is true when the operator is written in Go.
	IsOperatorGo bool
}

func (s *CRD) GetInput() (input.Input, error) {
	if s.Path == "" {
		fileName := fmt.Sprintf("%s_%s_%s_crd.yaml",
			strings.ToLower(s.Resource.Group),
			strings.ToLower(s.Resource.Version),
			s.Resource.LowerKind)
		s.Path = filepath.Join(CRDsDir, fileName)
	}
	initCache()
	return s.Input, nil
}

type fsCache struct {
	afero.Fs
}

func (c *fsCache) fileExists(path string) bool {
	_, err := c.Stat(path)
	return err == nil
}

var (
	// Global cache so users can use new CRD structs.
	cache *fsCache
	once  sync.Once
)

func initCache() {
	once.Do(func() {
		cache = &fsCache{Fs: afero.NewMemMapFs()}
	})
}

func (s *CRD) SetFS(_ afero.Fs) {}

func (s *CRD) CustomRender() ([]byte, error) {
	i, _ := s.GetInput()
	// controller-tools generates crd file names with no _crd.yaml suffix:
	// <group>_<version>_<kind>.yaml.
	path := strings.Replace(filepath.Base(i.Path), "_crd.yaml", ".yaml", 1)

	// controller-tools' generators read and make crds for all apis in pkg/apis,
	// so generate crds in a cached, in-memory fs to extract the data we need.
	if s.IsOperatorGo && !cache.fileExists(path) {
		g := &crdgenerator.Generator{
			RootPath:          s.AbsProjectPath,
			Domain:            strings.SplitN(s.Resource.FullGroup, ".", 2)[1],
			OutputDir:         ".",
			SkipMapValidation: false,
			OutFs:             cache,
		}
		if err := g.ValidateAndInitFields(); err != nil {
			return nil, err
		}
		if err := g.Do(); err != nil {
			return nil, err
		}
	}

	dstCRD := newCRDForResource(s.Resource)
	// Get our generated crd's from the in-memory fs. If it doesn't exist in the
	// fs, the corresponding API does not exist yet, so scaffold a fresh crd
	// without a validation spec.
	// If the crd exists in the fs, and a local crd exists, append the validation
	// spec. If a local crd does not exist, use the generated crd.
	if _, err := cache.Stat(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	} else if err == nil {
		b, err := afero.ReadFile(cache, path)
		if err != nil {
			return nil, err
		}
		dstCRD = &apiextv1beta1.CustomResourceDefinition{}
		if err = yaml.Unmarshal(b, dstCRD); err != nil {
			return nil, err
		}
		val := dstCRD.Spec.Validation.DeepCopy()

		// If the crd exists at i.Path, append the validation spec to its crd spec.
		if _, err := os.Stat(i.Path); err == nil {
			cb, err := ioutil.ReadFile(i.Path)
			if err != nil {
				return nil, err
			}
			if len(cb) > 0 {
				dstCRD = &apiextv1beta1.CustomResourceDefinition{}
				if err = yaml.Unmarshal(cb, dstCRD); err != nil {
					return nil, err
				}
				dstCRD.Spec.Validation = val
			}
		}
		// controller-tools does not set ListKind or Singular names.
		dstCRD.Spec.Names = getCRDNamesForResource(s.Resource)
		// Remove controller-tools default label.
		delete(dstCRD.Labels, "controller-tools.k8s.io")
	}
	addCRDSubresource(dstCRD)
	addCRDVersions(dstCRD)
	return k8sutil.GetObjectBytes(dstCRD)
}

func newCRDForResource(r *Resource) *apiextv1beta1.CustomResourceDefinition {
	return &apiextv1beta1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apiextensions.k8s.io/v1beta1",
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: r.Resource + "." + r.FullGroup,
		},
		Spec: apiextv1beta1.CustomResourceDefinitionSpec{
			Group:   r.FullGroup,
			Names:   getCRDNamesForResource(r),
			Scope:   apiextv1beta1.NamespaceScoped,
			Version: r.Version,
			Subresources: &apiextv1beta1.CustomResourceSubresources{
				Status: &apiextv1beta1.CustomResourceSubresourceStatus{},
			},
		},
	}
}

func getCRDNamesForResource(r *Resource) apiextv1beta1.CustomResourceDefinitionNames {
	return apiextv1beta1.CustomResourceDefinitionNames{
		Kind:     r.Kind,
		ListKind: r.Kind + "List",
		Plural:   r.Resource,
		Singular: r.LowerKind,
	}
}

func addCRDSubresource(crd *apiextv1beta1.CustomResourceDefinition) {
	if crd.Spec.Subresources == nil {
		crd.Spec.Subresources = &apiextv1beta1.CustomResourceSubresources{}
	}
	if crd.Spec.Subresources.Status == nil {
		crd.Spec.Subresources.Status = &apiextv1beta1.CustomResourceSubresourceStatus{}
	}
}

func addCRDVersions(crd *apiextv1beta1.CustomResourceDefinition) {
	// crd.Version is deprecated, use crd.Versions instead.
	var crdVersions []apiextv1beta1.CustomResourceDefinitionVersion
	if crd.Spec.Version != "" {
		var verExists, hasStorageVer bool
		for _, ver := range crd.Spec.Versions {
			if crd.Spec.Version == ver.Name {
				verExists = true
			}
			// There must be exactly one version flagged as a storage version.
			if ver.Storage {
				hasStorageVer = true
			}
		}
		if !verExists {
			crdVersions = []apiextv1beta1.CustomResourceDefinitionVersion{
				{Name: crd.Spec.Version, Served: true, Storage: !hasStorageVer},
			}
		}
	} else {
		crdVersions = []apiextv1beta1.CustomResourceDefinitionVersion{
			{Name: "v1alpha1", Served: true, Storage: true},
		}
	}

	if len(crd.Spec.Versions) > 0 {
		// crd.Version should always be the first element in crd.Versions.
		crd.Spec.Versions = append(crdVersions, crd.Spec.Versions...)
	} else {
		crd.Spec.Versions = crdVersions
	}
}
