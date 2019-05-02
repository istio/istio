// Copyright 2018 Istio Authors
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

package featurectl

import (
	"fmt"
	"io/ioutil"

	"github.com/ghodss/yaml"
)

type Feature struct {
	Name         string
	Description  string
	TargetLayers string
	Priority     int
	TestTypes    []string
	SourceFile   string `json:"-"`
}

type FeatureSet struct {
	Features []Feature
}

type FeatureLinter struct {
	features map[string]Feature
}

func (f *FeatureLinter) ProcessFile(filePath string) (err error) {
	var bytes []byte
	if bytes, err = ioutil.ReadFile(filePath); err != nil {
		return
	}
	var fs FeatureSet
	if err = yaml.Unmarshal(bytes, &fs); err != nil {
		return
	}
	for _, feature := range fs.Features {
		feature.SourceFile = filePath
		if err = f.ProcessFeature(feature); err != nil {
			return
		}
	}
	return nil
}

func (f *FeatureLinter) ProcessFeature(feature Feature) error {
	// golang has no way to initialize this map by default, afaict
	if f.features == nil {
		f.features = map[string]Feature{}
	}
	if dup, ok := f.features[feature.Name]; ok {
		return fmt.Errorf(fmt.Sprintf("Duplicate feature detected. %s is defined in %s and %s", feature.Name, feature.SourceFile, dup.SourceFile))
	}
	f.features[feature.Name] = feature
	if len(feature.Description) < 1 {
		return fmt.Errorf("Description is required")
	}
	return nil
}
