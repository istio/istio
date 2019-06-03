// Copyright 2019 Istio Authors
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

package helmreconciler

import (
	"context"
	"fmt"
	"strings"

	"github.com/ghodss/yaml"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/manifest"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/releaseutil"
	"k8s.io/helm/pkg/renderutil"
	"k8s.io/helm/pkg/tiller"
	"k8s.io/helm/pkg/timeconv"
	"k8s.io/kubernetes/pkg/kubectl"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (h *HelmReconciler) renderCharts(input RenderingInput) (ChartManifestsMap, error) {
	rawVals, err := yaml.Marshal(input.GetValues())
	if err != nil {
		return ChartManifestsMap{}, err
	}
	config := &chart.Config{Raw: string(rawVals), Values: map[string]*chart.Value{}}

	c, err := chartutil.Load(input.GetChartPath())
	if err != nil {
		return ChartManifestsMap{}, err
	}

	renderOpts := renderutil.Options{
		ReleaseOptions: chartutil.ReleaseOptions{
			// XXX: hard code or use icp.GetName()
			Name:      "istio",
			IsInstall: true,
			IsUpgrade: false,
			Time:      timeconv.Now(),
			Namespace: input.GetTargetNamespace(),
		},
		// XXX: hard-code or look this up somehow?
		KubeVersion: fmt.Sprintf("%s.%s", chartutil.DefaultKubeVersion.Major, chartutil.DefaultKubeVersion.Minor),
	}
	renderedTemplates, err := renderutil.Render(c, config, renderOpts)
	if err != nil {
		return ChartManifestsMap{}, err
	}

	return collectManifestsByChart(manifest.SplitManifests(renderedTemplates)), err
}

// collectManifestsByChart returns a map of chart->[]manifest.  names for subcharts
// will be of the form <root-name>/charts/<subchart-name>, e.g. istio/charts/galley
func collectManifestsByChart(manifests []manifest.Manifest) ChartManifestsMap {
	manifestsByChart := make(ChartManifestsMap)
	for _, chartManifest := range manifests {
		pathSegments := strings.Split(chartManifest.Name, "/")
		chartName := pathSegments[0]
		// paths always start with the root chart name and always have a template
		// name, so we should be safe not to check length
		if pathSegments[1] == "charts" {
			// subcharts will have names like <root-name>/charts/<subchart-name>/...
			chartName = strings.Join(pathSegments[:3], "/")
		}
		if _, ok := manifestsByChart[chartName]; !ok {
			manifestsByChart[chartName] = make([]manifest.Manifest, 0, 10)
		}
		manifestsByChart[chartName] = append(manifestsByChart[chartName], chartManifest)
	}
	for key, value := range manifestsByChart {
		manifestsByChart[key] = tiller.SortByKind(value)
	}
	return manifestsByChart
}

func (h *HelmReconciler) processManifests(manifests []manifest.Manifest) error {
	allErrors := []error{}
	origLogger := h.logger
	defer func() { h.logger = origLogger }()
	for _, manifest := range manifests {
		h.logger = origLogger.WithValues("manifest", manifest.Name)
		if !strings.HasSuffix(manifest.Name, ".yaml") {
			h.logger.V(2).Info("Skipping rendering of manifest")
			continue
		}
		h.logger.V(2).Info("Processing resources from manifest")
		// split the manifest into individual objects
		objects := releaseutil.SplitManifests(manifest.Content)
		for _, raw := range objects {
			rawJSON, err := yaml.YAMLToJSON([]byte(raw))
			if err != nil {
				h.logger.Error(err, "unable to convert raw data to JSON")
				allErrors = append(allErrors, err)
				continue
			}
			obj := &unstructured.Unstructured{}
			_, _, err = unstructured.UnstructuredJSONScheme.Decode(rawJSON, nil, obj)
			if err != nil {
				h.logger.Error(err, "unable to decode object into Unstructured")
				allErrors = append(allErrors, err)
				continue
			}
			err = h.processObject(obj)
			if err != nil {
				allErrors = append(allErrors, err)
			}
		}
	}

	return utilerrors.NewAggregate(allErrors)
}

func (h *HelmReconciler) processObject(obj *unstructured.Unstructured) error {
	if obj.GetKind() == "List" {
		allErrors := []error{}
		list, err := obj.ToList()
		if err != nil {
			h.logger.Error(err, "error converting List object")
			return err
		}
		for _, item := range list.Items {
			err = h.processObject(&item)
			if err != nil {
				allErrors = append(allErrors, err)
			}
		}
		return utilerrors.NewAggregate(allErrors)
	}

	mutatedObj, err := h.customizer.Listener().BeginResource(obj)
	if err != nil {
		h.logger.Error(err, "error preprocessing object")
		return err
	}

	err = kubectl.CreateApplyAnnotation(obj, unstructured.UnstructuredJSONScheme)
	if err != nil {
		h.logger.Error(err, "unexpected error adding apply annotation to object")
	}

	receiver := &unstructured.Unstructured{}
	receiver.SetGroupVersionKind(mutatedObj.GetObjectKind().GroupVersionKind())
	objectKey, _ := client.ObjectKeyFromObject(receiver)

	var patch Patch

	err = h.client.Get(context.TODO(), objectKey, receiver)
	if err != nil {
		if apierrors.IsNotFound(err) {
			h.logger.Info("creating resource")
			err = h.client.Create(context.TODO(), mutatedObj)
			if err == nil {
				// special handling
				if err = h.customizer.Listener().ResourceCreated(mutatedObj); err != nil {
					h.logger.Error(err, "unexpected error occurred during postprocessing of new resource")
				}
			} else {
				listenerErr := h.customizer.Listener().ResourceError(mutatedObj, err)
				if listenerErr != nil {
					h.logger.Error(listenerErr, "unexpected error occurred invoking ResourceError on listener")
				}
			}
		}
	} else if patch, err = h.CreatePatch(receiver, mutatedObj); err == nil && patch != nil {
		h.logger.Info("updating existing resource")
		mutatedObj, err = patch.Apply()
		if err == nil {
			if err = h.customizer.Listener().ResourceUpdated(mutatedObj, receiver); err != nil {
				h.logger.Error(err, "unexpected error occurred during postprocessing of updated resource")
			}
		} else {
			listenerErr := h.customizer.Listener().ResourceError(mutatedObj, err)
			if listenerErr != nil {
				h.logger.Error(listenerErr, "unexpected error occurred invoking ResourceError on listener")
			}
		}
	}
	h.logger.V(2).Info("resource reconciliation complete")
	if err != nil {
		h.logger.Error(err, "error occurred reconciling resource")
	}
	return err
}
