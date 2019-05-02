// Copyright 2019 The Operator-SDK Authors
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

package watches

import (
	"errors"
	"fmt"
	"io/ioutil"

	yaml "gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/helm/pkg/chartutil"
)

// Watch defines options for configuring a watch for a Helm-based
// custom resource.
type Watch struct {
	GroupVersionKind        schema.GroupVersionKind
	ChartDir                string
	WatchDependentResources bool
}

type yamlWatch struct {
	Group                   string `yaml:"group"`
	Version                 string `yaml:"version"`
	Kind                    string `yaml:"kind"`
	Chart                   string `yaml:"chart"`
	WatchDependentResources bool   `yaml:"watchDependentResources"`
}

func (w *yamlWatch) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// by default, the operator will watch dependent resources
	w.WatchDependentResources = true

	// hide watch data in plain struct to prevent unmarshal from calling
	// UnmarshalYAML again
	type plain yamlWatch

	return unmarshal((*plain)(w))
}

// Load loads a slice of Watches from the watch file at `path`. For each entry
// in the watches file, it verifies the configuration. If an error is
// encountered loading the file or verifying the configuration, it will be
// returned.
func Load(path string) ([]Watch, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	yamlWatches := []yamlWatch{}
	err = yaml.Unmarshal(b, &yamlWatches)
	if err != nil {
		return nil, err
	}

	watches := []Watch{}
	watchesMap := make(map[schema.GroupVersionKind]Watch)
	for _, w := range yamlWatches {
		gvk := schema.GroupVersionKind{
			Group:   w.Group,
			Version: w.Version,
			Kind:    w.Kind,
		}

		if err := verifyGVK(gvk); err != nil {
			return nil, fmt.Errorf("invalid GVK: %s: %s", gvk, err)
		}

		if _, err := chartutil.IsChartDir(w.Chart); err != nil {
			return nil, fmt.Errorf("invalid chart directory %s: %s", w.Chart, err)
		}

		if _, ok := watchesMap[gvk]; ok {
			return nil, fmt.Errorf("duplicate GVK: %s", gvk)
		}
		watch := Watch{
			GroupVersionKind:        gvk,
			ChartDir:                w.Chart,
			WatchDependentResources: w.WatchDependentResources,
		}
		watchesMap[gvk] = watch
		watches = append(watches, watch)
	}
	return watches, nil
}

func verifyGVK(gvk schema.GroupVersionKind) error {
	// A GVK without a group is valid. Certain scenarios may cause a GVK
	// without a group to fail in other ways later in the initialization
	// process.
	if gvk.Version == "" {
		return errors.New("version must not be empty")
	}
	if gvk.Kind == "" {
		return errors.New("kind must not be empty")
	}
	return nil
}
