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

package engine

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/releaseutil"
	"k8s.io/helm/pkg/tiller/environment"
)

// OwnerRefEngine wraps a tiller Render engine, adding ownerrefs to rendered assets
type OwnerRefEngine struct {
	environment.Engine
	refs []metav1.OwnerReference
}

// assert interface
var _ environment.Engine = &OwnerRefEngine{}

// Render proxies to the wrapped Render engine and then adds ownerRefs to each rendered file
func (o *OwnerRefEngine) Render(chart *chart.Chart, values chartutil.Values) (map[string]string, error) {
	rendered, err := o.Engine.Render(chart, values)
	if err != nil {
		return nil, err
	}

	ownedRenderedFiles := map[string]string{}
	for fileName, renderedFile := range rendered {
		if !strings.HasSuffix(fileName, ".yaml") {
			// Pass non-YAML files through untouched.
			// This is required for NOTES.txt
			ownedRenderedFiles[fileName] = renderedFile
			continue
		}
		withOwner, err := o.addOwnerRefs(renderedFile)
		if err != nil {
			return nil, err
		}
		if withOwner == "" {
			continue
		}
		ownedRenderedFiles[fileName] = withOwner
	}
	return ownedRenderedFiles, nil
}

// addOwnerRefs adds the configured ownerRefs to a single rendered file
// Adds the ownerrefs to all the documents in a YAML file
func (o *OwnerRefEngine) addOwnerRefs(fileContents string) (string, error) {
	const documentSeparator = "---\n"
	var outBuf bytes.Buffer

	manifests := releaseutil.SplitManifests(fileContents)
	for _, manifest := range sortManifests(manifests) {
		manifestMap := chartutil.FromYaml(manifest)
		if errors, ok := manifestMap["Error"]; ok {
			return "", fmt.Errorf("error parsing rendered template to add ownerrefs: %v", errors)
		}

		// Check if the document is empty
		if len(manifestMap) == 0 {
			continue
		}

		unst, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&manifestMap)
		if err != nil {
			return "", err
		}

		unstructured := &unstructured.Unstructured{Object: unst}
		unstructured.SetOwnerReferences(o.refs)

		// Write the document with owner ref to the buffer
		// Also add document start marker
		_, err = outBuf.WriteString(documentSeparator + chartutil.ToYaml(unstructured.Object))
		if err != nil {
			return "", fmt.Errorf("error writing the document to buffer: %v", err)
		}
	}

	return outBuf.String(), nil
}

// NewOwnerRefEngine creates a new OwnerRef engine with a set of metav1.OwnerReferences to be added to assets
func NewOwnerRefEngine(baseEngine environment.Engine, refs []metav1.OwnerReference) environment.Engine {
	return &OwnerRefEngine{
		Engine: baseEngine,
		refs:   refs,
	}
}

func sortManifests(in map[string]string) []string {
	var keys []string
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var manifests []string
	for _, k := range keys {
		manifests = append(manifests, in[k])
	}
	return manifests
}
