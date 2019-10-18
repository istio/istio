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

	"github.com/ghodss/yaml"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/helm/pkg/manifest"
	"k8s.io/helm/pkg/releaseutil"
	kubectl "k8s.io/kubectl/pkg/util"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/component/controlplane"
	"istio.io/operator/pkg/helm"
	istiomanifest "istio.io/operator/pkg/manifest"
	"istio.io/operator/pkg/name"
	"istio.io/operator/pkg/translate"
	"istio.io/operator/pkg/util"
	"istio.io/operator/pkg/validate"
	binversion "istio.io/operator/version"
	"istio.io/pkg/log"
)

func (h *HelmReconciler) renderCharts(in RenderingInput) (ChartManifestsMap, error) {
	icp, ok := in.GetInputConfig().(*v1alpha2.IstioControlPlane)
	if !ok {
		return nil, fmt.Errorf("unexpected type %T in renderCharts", in.GetInputConfig())
	}

	icpSpec := icp.GetSpec()
	if err := validate.CheckIstioControlPlaneSpec(icpSpec, false); err != nil {
		return nil, err
	}

	mergedICPS, err := mergeICPSWithProfile(icpSpec)
	if err != nil {
		return nil, err
	}

	t, err := translate.NewTranslator(binversion.OperatorBinaryVersion.MinorVersion)
	if err != nil {
		return nil, err
	}

	cp := controlplane.NewIstioControlPlane(mergedICPS, t)
	if err := cp.Run(); err != nil {
		return nil, fmt.Errorf("failed to create Istio control plane with spec: \n%v\nerror: %s", mergedICPS, err)
	}

	manifests, errs := cp.RenderManifest()
	if errs != nil {
		err = errs.ToError()
	}

	return toChartManifestsMap(manifests), err
}

// mergeICPSWithProfile overlays the values in icp on top of the defaults for the profile given by icp.profile and
// returns the merged result.
func mergeICPSWithProfile(icp *v1alpha2.IstioControlPlaneSpec) (*v1alpha2.IstioControlPlaneSpec, error) {
	profile := icp.Profile

	// This contains the IstioControlPlane CR.
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
		baseCRYAML, err = helm.OverlayYAML(defaultYAML, baseCRYAML)
		if err != nil {
			return nil, fmt.Errorf("could not overlay the profile over the default %s: %s", profile, err)
		}
	}

	_, baseYAML, err := unmarshalAndValidateICP(baseCRYAML)
	if err != nil {
		return nil, err
	}

	overlayYAML, err := util.MarshalWithJSONPB(icp)
	if err != nil {
		return nil, err
	}

	// Merge base and overlay.
	mergedYAML, err := helm.OverlayYAML(baseYAML, overlayYAML)
	if err != nil {
		return nil, fmt.Errorf("could not overlay user config over base: %s", err)
	}
	return unmarshalAndValidateICPSpec(mergedYAML)
}

// unmarshalAndValidateICP unmarshals the IstioControlPlane in the crYAML string and validates it.
// If successful, it returns both a struct and string YAML representations of the IstioControlPlaneSpec embedded in icp.
func unmarshalAndValidateICP(crYAML string) (*v1alpha2.IstioControlPlaneSpec, string, error) {
	// TODO: add GroupVersionKind handling as appropriate.
	if crYAML == "" {
		return &v1alpha2.IstioControlPlaneSpec{}, "", nil
	}
	icps, _, err := istiomanifest.ParseK8SYAMLToIstioControlPlaneSpec(crYAML)
	if err != nil {
		return nil, "", fmt.Errorf("could not parse the overlay file: %s\n\nOriginal YAML:\n%s", err, crYAML)
	}
	if errs := validate.CheckIstioControlPlaneSpec(icps, false); len(errs) != 0 {
		return nil, "", fmt.Errorf("input file failed validation with the following errors: %s\n\nOriginal YAML:\n%s", errs, crYAML)
	}
	icpsYAML, err := util.MarshalWithJSONPB(icps)
	if err != nil {
		return nil, "", fmt.Errorf("could not marshal: %s", err)
	}
	return icps, icpsYAML, nil
}

// unmarshalAndValidateICPSpec unmarshals the IstioControlPlaneSpec in the icpsYAML string and validates it.
// If successful, it returns a struct representation of icpsYAML.
func unmarshalAndValidateICPSpec(icpsYAML string) (*v1alpha2.IstioControlPlaneSpec, error) {
	icps := &v1alpha2.IstioControlPlaneSpec{}
	if err := util.UnmarshalWithJSONPB(icpsYAML, icps); err != nil {
		return nil, fmt.Errorf("could not unmarshal the merged YAML: %s\n\nYAML:\n%s", err, icpsYAML)
	}
	if errs := validate.CheckIstioControlPlaneSpec(icps, true); len(errs) != 0 {
		return nil, fmt.Errorf(errs.Error())
	}
	return icps, nil
}

func (h *HelmReconciler) ProcessManifest(manifest manifest.Manifest) error {
	var errs []error
	log.Info("Processing resources from manifest")
	// split the manifest into individual objects
	objects := releaseutil.SplitManifests(manifest.Content)
	for _, raw := range objects {
		rawJSON, err := yaml.YAMLToJSON([]byte(raw))
		if err != nil {
			log.Errorf("unable to convert raw data to JSON: %s", err)
			errs = append(errs, err)
			continue
		}
		obj := &unstructured.Unstructured{}
		_, _, err = unstructured.UnstructuredJSONScheme.Decode(rawJSON, nil, obj)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		err = h.ProcessObject(obj)
		if err != nil {
			errs = append(errs, err)
		}
	}

	return utilerrors.NewAggregate(errs)
}

func (h *HelmReconciler) ProcessObject(obj *unstructured.Unstructured) error {
	if obj.GetKind() == "List" {
		allErrors := []error{}
		list, err := obj.ToList()
		if err != nil {
			log.Errorf("error converting List object: %s", err)
			return err
		}
		for _, item := range list.Items {
			err = h.ProcessObject(&item)
			if err != nil {
				allErrors = append(allErrors, err)
			}
		}
		return utilerrors.NewAggregate(allErrors)
	}

	mutatedObj, err := h.customizer.Listener().BeginResource(obj)
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
	} else if patch, err = h.CreatePatch(receiver, mutatedObj); err == nil && patch != nil {
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
			Content: v,
		}}
	}
	return out
}
