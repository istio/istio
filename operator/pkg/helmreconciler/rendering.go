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
	"sync"

	"istio.io/istio/operator/pkg/tpath"

	util2 "k8s.io/kubectl/pkg/util"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/helm/pkg/manifest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"istio.io/api/operator/v1alpha1"
	valuesv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/component/controlplane"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/validate"
	binversion "istio.io/istio/operator/version"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

// ObjectCache is a cache of objects.
type ObjectCache struct {
	// cache is a cache keyed by object Hash() function.
	cache map[string]*object.K8sObject
	mu    *sync.RWMutex
}

var (
	// objectCaches holds the latest copy of each object applied by the controller, keyed by the IstioOperator CR name
	// and the object Hash() function.
	objectCaches   = make(map[string]*ObjectCache)
	objectCachesMu sync.RWMutex
)

// FlushObjectCaches flushes all K8s object caches.
func FlushObjectCaches() {
	objectCachesMu.Lock()
	defer objectCachesMu.Unlock()
	objectCaches = make(map[string]*ObjectCache)
}

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
	baseIOPYAML, err := helm.ReadProfileYAML(profile)
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
		baseIOPYAML, err = util.OverlayYAML(defaultYAML, baseIOPYAML)
		if err != nil {
			return nil, fmt.Errorf("could not overlay the profile over the default %s: %s", profile, err)
		}
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
		baseIOPYAML, err = util.OverlayYAML(baseIOPYAML, buildHubTagOverlayYAML)
		if err != nil {
			return nil, err
		}
	}

	overlayYAML, err := util.MarshalWithJSONPB(iop)
	if err != nil {
		return nil, err
	}
	baseYAML, err := tpath.GetSpecSubtree(baseIOPYAML)
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

// unmarshalAndValidateIOPSpec unmarshals the IstioOperatorSpec in the iopsYAML string and validates it.
// If successful, it returns a struct representation of iopsYAML.
func unmarshalAndValidateIOPSpec(iopsYAML string) (*v1alpha1.IstioOperatorSpec, error) {
	iops := &v1alpha1.IstioOperatorSpec{}
	if err := util.UnmarshalWithJSONPB(iopsYAML, iops, false); err != nil {
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
	crName := h.instance.Name + "-" + manifest.Name
	log.Infof("Processing resources from manifest: %s for CR %s", manifest.Name, crName)
	allObjects, err := object.ParseK8sObjectsFromYAMLManifest(manifest.Content)
	if err != nil {
		return 0, err
	}

	objectCachesMu.Lock()

	// Create and/or get the cache corresponding to the CR crName we're processing. Per crName partitioning is required to
	// prune the cache to remove any objects not in the manifest generated for a given CR.
	if objectCaches[crName] == nil {
		objectCaches[crName] = &ObjectCache{
			cache: make(map[string]*object.K8sObject),
			mu:    &sync.RWMutex{},
		}
	}
	objectCache := objectCaches[crName]

	objectCachesMu.Unlock()

	// Ensure that for a given CR crName only one control loop uses the per-crName cache at any time.
	objectCache.mu.Lock()
	defer objectCache.mu.Unlock()

	// No further locking required beyond this point, since we have a ptr to a cache corresponding to a CR crName and no
	// other controller is allowed to work on at the same time.
	var deployedObjects int
	var changedObjects object.K8sObjects
	var changedObjectKeys []string
	allObjectsMap := make(map[string]bool)

	// Check which objects in the manifest have changed from those in the cache.
	for _, obj := range allObjects {
		oh := obj.Hash()
		allObjectsMap[oh] = true
		if co, ok := objectCache.cache[oh]; ok && obj.Equal(co) {
			// Object is in the cache and unchanged.
			log.Infof("Object %s is unchanged, skip update.", oh)
			deployedObjects++
			continue
		}
		changedObjects = append(changedObjects, obj)
		changedObjectKeys = append(changedObjectKeys, oh)
	}

	if len(changedObjectKeys) > 0 {
		log.Infof("Changed object list: \n - %s", strings.Join(changedObjectKeys, "\n - "))
	} else {
		log.Infof("No objects changed for this component.")
	}

	// For each changed object, write it to the API server.
	for _, obj := range changedObjects {
		err = h.ProcessObject(manifest.Name, obj.UnstructuredObject())
		if err != nil {
			log.Error(err.Error())
			errs = append(errs, err)
			continue
		}
		deployedObjects++
		log.Infof("Adding object %s to cache.", obj.Hash())
		// Update the cache with the latest object.
		objectCache.cache[obj.Hash()] = obj
	}

	// Prune anything not in the manifest out of the cache.
	var removeKeys []string
	for k := range objectCache.cache {
		if !allObjectsMap[k] {
			removeKeys = append(removeKeys, k)
		}
	}
	for _, k := range removeKeys {
		log.Infof("Pruning object %s from cache.", k)
		delete(objectCache.cache, k)
	}

	return deployedObjects, utilerrors.NewAggregate(errs)
}

// ProcessObject creates or updates an object in the API server depending on whether it already exists.
// It mutates obj.
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

	err = util2.CreateApplyAnnotation(obj, unstructured.UnstructuredJSONScheme)
	if err != nil {
		log.Errorf("unexpected error adding apply annotation to object: %s", err)
	}

	receiver := &unstructured.Unstructured{}
	receiver.SetGroupVersionKind(mutatedObj.GetObjectKind().GroupVersionKind())
	objectKey, _ := client.ObjectKeyFromObject(mutatedObj)

	if err = h.client.Get(context.TODO(), objectKey, receiver); apierrors.IsNotFound(err) {
		log.Infof("creating resource: %s", objectKey)
		return h.client.Create(context.TODO(), mutatedObj)
	} else if err == nil {
		log.Infof("updating resource: %s", objectKey)
		updatedAccessor, err := meta.Accessor(mutatedObj)
		if err != nil {
			return fmt.Errorf("cannot create object accessor for mutatedObj:\n%v", mutatedObj)
		}
		updatedAccessor.SetResourceVersion(receiver.GetResourceVersion())
		return h.client.Update(context.TODO(), mutatedObj)
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
