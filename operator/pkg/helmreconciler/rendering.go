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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/helm/pkg/manifest"
	kubectl "k8s.io/kubectl/pkg/util"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"istio.io/api/operator/v1alpha1"
	valuesv1alpha1 "istio.io/operator/pkg/apis/istio/v1alpha1"
	"istio.io/operator/pkg/component/controlplane"
	"istio.io/operator/pkg/helm"
	istiomanifest "istio.io/operator/pkg/manifest"
	"istio.io/operator/pkg/name"
	"istio.io/operator/pkg/object"
	"istio.io/operator/pkg/translate"
	"istio.io/operator/pkg/util"
	"istio.io/operator/pkg/validate"
	binversion "istio.io/operator/version"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

func (h *HelmReconciler) renderCharts(in RenderingInput) (ChartManifestsMap, error) {
	iop, ok := in.GetInputConfig().(*valuesv1alpha1.IstioOperator)
	if !ok {
		return nil, fmt.Errorf("unexpected type %T in renderCharts", in.GetInputConfig())
	}
	iopSpec := iop.Spec
	if err := validate.CheckIstioOperatorSpec(iopSpec, false); err != nil {
		return nil, err
	}

	mergedIOPS, err := MergeIOPSWithProfile(iopSpec)
	if err != nil {
		return nil, err
	}

	t, err := translate.NewTranslator(binversion.OperatorBinaryVersion.MinorVersion)
	if err != nil {
		return nil, err
	}

	cp, err := controlplane.NewIstioOperator(mergedIOPS, t)
	if err != nil {
		return nil, err
	}
	if err := cp.Run(); err != nil {
		return nil, fmt.Errorf("failed to create Istio control plane with spec: \n%v\nerror: %s", mergedIOPS, err)
	}

	manifests, errs := cp.RenderManifest()
	if errs != nil {
		err = errs.ToError()
	}

	return toChartManifestsMap(manifests), err
}

// MergeIOPSWithProfile overlays the values in iop on top of the defaults for the profile given by iop.profile and
// returns the merged result.
func MergeIOPSWithProfile(iop *v1alpha1.IstioOperatorSpec) (*v1alpha1.IstioOperatorSpec, error) {
	profile := iop.Profile

	// This contains the IstioOperator CR.
	baseCRYAML, err := helm.ReadProfileYAML(profile)
	if err != nil {
		return nil, fmt.Errorf("could not read the profile values for %s: %s", profile, err)
	}

	if !helm.IsDefaultProfile(profile) {
		// Profile definitions are relative to the default profile, so read that first.
		dfn, err := helm.DefaultFilenameForProfile(profile)
		if err != nil {
			return nil, err
		}
		defaultYAML, err := helm.ReadProfileYAML(dfn)
		if err != nil {
			return nil, fmt.Errorf("could not read the default profile values for %s: %s", dfn, err)
		}
		baseCRYAML, err = util.OverlayYAML(defaultYAML, baseCRYAML)
		if err != nil {
			return nil, fmt.Errorf("could not overlay the profile over the default %s: %s", profile, err)
		}
	}

	_, baseYAML, err := unmarshalAndValidateIOP(baseCRYAML)
	if err != nil {
		return nil, err
	}

	// Due to the fact that base profile is compiled in before a tag can be created, we must allow an additional
	// override from variables that are set during release build time.
	hub := version.DockerInfo.Hub
	tag := version.DockerInfo.Tag
	if hub != "" && hub != "unknown" && tag != "" && tag != "unknown" {
		buildHubTagOverlayYAML, err := helm.GenerateHubTagOverlay(hub, tag)
		if err != nil {
			return nil, err
		}
		baseYAML, err = util.OverlayYAML(baseYAML, buildHubTagOverlayYAML)
		if err != nil {
			return nil, err
		}
	}

	overlayYAML, err := util.MarshalWithJSONPB(iop)
	if err != nil {
		return nil, err
	}

	// Merge base and overlay.
	mergedYAML, err := util.OverlayYAML(baseYAML, overlayYAML)
	if err != nil {
		return nil, fmt.Errorf("could not overlay user config over base: %s", err)
	}
	return unmarshalAndValidateIOPSpec(mergedYAML)
}

// unmarshalAndValidateIOP unmarshals the IstioOperator in the crYAML string and validates it.
// If successful, it returns both a struct and string YAML representations of the IstioOperatorSpec embedded in iop.
func unmarshalAndValidateIOP(crYAML string) (*v1alpha1.IstioOperatorSpec, string, error) {
	// TODO: add GroupVersionKind handling as appropriate.
	if crYAML == "" {
		return &v1alpha1.IstioOperatorSpec{}, "", nil
	}
	iops, _, err := istiomanifest.ParseK8SYAMLToIstioOperatorSpec(crYAML)
	if err != nil {
		return nil, "", fmt.Errorf("could not parse the overlay file: %s\n\nOriginal YAML:\n%s", err, crYAML)
	}
	if errs := validate.CheckIstioOperatorSpec(iops, false); len(errs) != 0 {
		return nil, "", fmt.Errorf("input file failed validation with the following errors: %s\n\nOriginal YAML:\n%s", errs, crYAML)
	}
	iopsYAML, err := util.MarshalWithJSONPB(iops)
	if err != nil {
		return nil, "", fmt.Errorf("could not marshal: %s", err)
	}
	return iops, iopsYAML, nil
}

// unmarshalAndValidateIOPSpec unmarshals the IstioOperatorSpec in the iopsYAML string and validates it.
// If successful, it returns a struct representation of iopsYAML.
func unmarshalAndValidateIOPSpec(iopsYAML string) (*v1alpha1.IstioOperatorSpec, error) {
	iops := &v1alpha1.IstioOperatorSpec{}
	if err := util.UnmarshalWithJSONPB(iopsYAML, iops); err != nil {
		return nil, fmt.Errorf("could not unmarshal the merged YAML: %s\n\nYAML:\n%s", err, iopsYAML)
	}
	if errs := validate.CheckIstioOperatorSpec(iops, true); len(errs) != 0 {
		return nil, fmt.Errorf(errs.Error())
	}
	return iops, nil
}

// ProcessManifest apply the manifest to create or update resources, returns the number of objects processed
func (h *HelmReconciler) ProcessManifest(manifest manifest.Manifest) (int, error) {
	var errs []error
	log.Infof("Processing resources from manifest: %s", manifest.Name)
	objects, err := object.ParseK8sObjectsFromYAMLManifest(manifest.Content)
	if err != nil {
		return 0, err
	}
	for _, obj := range objects {
		err = h.ProcessObject(manifest.Name, obj.UnstructuredObject())
		if err != nil {
			errs = append(errs, err)
		}
	}
	return len(objects), utilerrors.NewAggregate(errs)
}

func (h *HelmReconciler) ProcessObject(chartName string, obj *unstructured.Unstructured) error {
	if obj.GetKind() == "List" {
		allErrors := []error{}
		list, err := obj.ToList()
		if err != nil {
			log.Errorf("error converting List object: %s", err)
			return err
		}
		for _, item := range list.Items {
			err = h.ProcessObject(chartName, &item)
			if err != nil {
				allErrors = append(allErrors, err)
			}
		}
		return utilerrors.NewAggregate(allErrors)
	}

	mutatedObj, err := h.customizer.Listener().BeginResource(chartName, obj)
	if err != nil {
		log.Errorf("error preprocessing object: %s", err)
		return err
	}

	err = kubectl.CreateApplyAnnotation(obj, unstructured.UnstructuredJSONScheme)
	if err != nil {
		log.Errorf("unexpected error adding apply annotation to object: %s", err)
	}

	receiver := &unstructured.Unstructured{}
	receiver.SetGroupVersionKind(mutatedObj.GetObjectKind().GroupVersionKind())
	objectKey, _ := client.ObjectKeyFromObject(mutatedObj)

	var patch Patch

	err = h.client.Get(context.TODO(), objectKey, receiver)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Infof("creating resource: %s", objectKey)
			err = h.client.Create(context.TODO(), mutatedObj)
			if err == nil {
				// special handling
				if err = h.customizer.Listener().ResourceCreated(mutatedObj); err != nil {
					log.Errorf("unexpected error occurred during postprocessing of new resource: %s", err)
				}
			} else {
				listenerErr := h.customizer.Listener().ResourceError(mutatedObj, err)
				if listenerErr != nil {
					log.Errorf("unexpected error occurred invoking ResourceError on listener: %s", listenerErr)
				}
			}
		}
	} else if h.needUpdateAndPrune {
		if patch, err = h.CreatePatch(receiver, mutatedObj); err == nil && patch != nil {
			log.Info("updating existing resource")
			mutatedObj, err = patch.Apply()
			if err == nil {
				if err = h.customizer.Listener().ResourceUpdated(mutatedObj, receiver); err != nil {
					log.Errorf("unexpected error occurred during postprocessing of updated resource: %s", err)
				}
			} else {
				listenerErr := h.customizer.Listener().ResourceError(obj, err)
				if listenerErr != nil {
					log.Errorf("unexpected error occurred invoking ResourceError on listener: %s", listenerErr)
				}
			}
		}
	}
	if err != nil {
		log.Errorf("error occurred reconciling resource: %s", err)
	}
	return err
}

func toChartManifestsMap(m name.ManifestMap) ChartManifestsMap {
	out := make(ChartManifestsMap)
	for k, v := range m {
		out[string(k)] = []manifest.Manifest{{
			Name:    string(k),
			Content: strings.Join(v, helm.YAMLSeparator),
		}}
	}
	return out
}
