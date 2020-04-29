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
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"istio.io/api/operator/v1alpha1"
	valuesv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
)

type componentNameToListMap map[name.ComponentName][]name.ComponentName
type componentTree map[name.ComponentName]interface{}

const (
	// Time to wait for internal dependencies before proceeding to installing the next component.
	internalDepTimeout = 10 * time.Minute
)

var (
	componentDependencies = componentNameToListMap{
		name.PilotComponentName: {
			name.PolicyComponentName,
			name.TelemetryComponentName,
			name.CNIComponentName,
			name.IngressComponentName,
			name.EgressComponentName,
			name.AddonComponentName,
		},
		name.IstioBaseComponentName: {
			name.PilotComponentName,
		},
	}

	installTree      = make(componentTree)
	dependencyWaitCh = make(map[name.ComponentName]chan struct{})
)

func init() {
	buildInstallTree()
	for _, parent := range componentDependencies {
		for _, child := range parent {
			dependencyWaitCh[child] = make(chan struct{}, 1)
		}
	}

}

// HelmReconciler reconciles resources rendered by a set of helm charts.
type HelmReconciler struct {
	client     client.Client
	restConfig *rest.Config
	clientSet  *kubernetes.Clientset
	iop        *valuesv1alpha1.IstioOperator
	opts       *Options
	// copy of the last generated manifests.
	manifests name.ManifestMap
}

// Options are options for HelmReconciler.
type Options struct {
	// DryRun executes all actions but does not write anything to the cluster.
	DryRun bool
	// Log is a console logger for user visible CLI output.
	Log clog.Logger
	// Wait determines if we will wait for resources to be fully applied.
	Wait        bool
	ProgressLog *util.ProgressLog
}

var defaultOptions = &Options{
	Log:         clog.NewDefaultLogger(),
	ProgressLog: util.NewProgressLog(),
}

// NewHelmReconciler creates a HelmReconciler and returns a ptr to it
func NewHelmReconciler(client client.Client, restConfig *rest.Config, iop *valuesv1alpha1.IstioOperator, opts *Options) (*HelmReconciler, error) {
	if opts == nil {
		opts = defaultOptions
	}
	if opts.ProgressLog == nil {
		opts.ProgressLog = util.NewProgressLog()
	}
	if iop == nil {
		// allows controller code to function for cases where IOP is not provided (e.g. operator remove).
		iop = &valuesv1alpha1.IstioOperator{}
		iop.Spec = &v1alpha1.IstioOperatorSpec{}
	}
	var cs *kubernetes.Clientset
	var err error
	if restConfig != nil {
		cs, err = kubernetes.NewForConfig(restConfig)
	}
	if err != nil {
		return nil, err
	}
	return &HelmReconciler{
		client:     client,
		restConfig: restConfig,
		clientSet:  cs,
		iop:        iop,
		opts:       opts,
	}, nil
}

// Reconcile reconciles the associated resources.
func (h *HelmReconciler) Reconcile() (*v1alpha1.InstallStatus, error) {
	manifestMap, err := h.RenderCharts()
	if err != nil {
		return nil, err
	}

	return h.processRecursive(manifestMap), h.Prune(manifestMap)
}

// processRecursive processes the given manifests in an order of dependencies defined in h. Dependencies are a tree,
// where a child must wait for the parent to complete before starting.
func (h *HelmReconciler) processRecursive(manifests ChartManifestsMap) *v1alpha1.InstallStatus {
	componentStatus := make(map[string]*v1alpha1.InstallStatus_VersionStatus)

	// mu protects the shared InstallStatus componentStatus across goroutines
	var mu sync.Mutex
	// wg waits for all manifest processing goroutines to finish
	var wg sync.WaitGroup

	for c, m := range manifests {
		c, m := c, m
		wg.Add(1)
		go func() {
			var processedObjs object.K8sObjects
			var deployedObjects int
			defer wg.Done()
			cn := name.ComponentName(c)
			if s := dependencyWaitCh[cn]; s != nil {
				scope.Infof("%s is waiting on dependency...", c)
				<-s
				scope.Infof("Dependency for %s has completed, proceeding.", c)
			}

			// Possible paths for status are RECONCILING -> {NONE, ERROR, HEALTHY}. NONE means component has no resources.
			// In NONE case, the component is not shown in overall status.
			mu.Lock()
			setStatus(componentStatus, c, v1alpha1.InstallStatus_RECONCILING, nil)
			mu.Unlock()

			status := v1alpha1.InstallStatus_NONE
			var err error
			if len(m) != 0 {
				processedObjs, deployedObjects, err = h.ProcessManifest(m, len(componentDependencies[cn]) > 0)
				if err != nil {
					status = v1alpha1.InstallStatus_ERROR
				} else if len(processedObjs) != 0 || deployedObjects > 0 {
					status = v1alpha1.InstallStatus_HEALTHY
				}
			}

			mu.Lock()
			setStatus(componentStatus, c, status, err)
			mu.Unlock()

			// Signal all the components that depend on us.
			for _, ch := range componentDependencies[cn] {
				scope.Infof("Unblocking dependency %s.", ch)
				dependencyWaitCh[ch] <- struct{}{}
			}
		}()
	}
	wg.Wait()

	out := &v1alpha1.InstallStatus{
		Status:          overallStatus(componentStatus),
		ComponentStatus: componentStatus,
	}

	return out
}

// Delete resources associated with the custom resource instance
func (h *HelmReconciler) Delete() error {
	manifestMap, err := h.RenderCharts()
	if err != nil {
		return err
	}
	return h.Prune(manifestMap)
}

// SetStatusBegin updates the status field on the IstioOperator instance before reconciling.
func (h *HelmReconciler) SetStatusBegin() error {
	isop := &valuesv1alpha1.IstioOperator{}
	namespacedName := types.NamespacedName{
		Name:      h.iop.Name,
		Namespace: h.iop.Namespace,
	}
	if err := h.GetClient().Get(context.TODO(), namespacedName, isop); err != nil {
		if runtime.IsNotRegisteredError(err) {
			// CRD not yet installed in cluster, nothing to update.
			return nil
		}
		return fmt.Errorf("failed to get IstioOperator before updating status due to %v", err)
	}
	if isop.Status == nil {
		isop.Status = &v1alpha1.InstallStatus{Status: v1alpha1.InstallStatus_RECONCILING}
	} else {
		cs := isop.Status.ComponentStatus
		for cn := range cs {
			cs[cn] = &v1alpha1.InstallStatus_VersionStatus{
				Status: v1alpha1.InstallStatus_RECONCILING,
			}
		}
		isop.Status.Status = v1alpha1.InstallStatus_RECONCILING
	}
	return h.GetClient().Status().Update(context.TODO(), isop)
}

// SetStatusComplete updates the status field on the IstioOperator instance based on the resulting err parameter.
func (h *HelmReconciler) SetStatusComplete(status *v1alpha1.InstallStatus) error {
	iop := &valuesv1alpha1.IstioOperator{}
	namespacedName := types.NamespacedName{
		Name:      h.iop.Name,
		Namespace: h.iop.Namespace,
	}
	if err := h.GetClient().Get(context.TODO(), namespacedName, iop); err != nil {
		return fmt.Errorf("failed to get IstioOperator before updating status due to %v", err)
	}
	iop.Status = status
	return h.GetClient().Status().Update(context.TODO(), iop)
}

// setStatus sets the status for the component with the given name, which is a key in the given map.
// If the status is InstallStatus_NONE, the component name is deleted from the map.
// Otherwise, if the map key/value is missing, one is created.
func setStatus(s map[string]*v1alpha1.InstallStatus_VersionStatus, componentName string, status v1alpha1.InstallStatus_Status, err error) {
	if status == v1alpha1.InstallStatus_NONE {
		delete(s, componentName)
		return
	}
	if _, ok := s[componentName]; !ok {
		s[componentName] = &v1alpha1.InstallStatus_VersionStatus{}
	}
	s[componentName].Status = status
	if err != nil {
		s[componentName].Error = err.Error()
	}
}

// overallStatus returns the summary status over all components.
// - If all components are HEALTHY, overall status is HEALTHY.
// - If one or more components are RECONCILING and others are HEALTHY, overall status is RECONCILING.
// - If one or more components are UPDATING and others are HEALTHY, overall status is UPDATING.
// - If components are a mix of RECONCILING, UPDATING and HEALTHY, overall status is UPDATING.
// - If any component is in ERROR state, overall status is ERROR.
func overallStatus(componentStatus map[string]*v1alpha1.InstallStatus_VersionStatus) v1alpha1.InstallStatus_Status {
	ret := v1alpha1.InstallStatus_HEALTHY
	for _, cs := range componentStatus {
		if cs.Status == v1alpha1.InstallStatus_ERROR {
			ret = v1alpha1.InstallStatus_ERROR
			break
		} else if cs.Status == v1alpha1.InstallStatus_UPDATING {
			ret = v1alpha1.InstallStatus_UPDATING
			break
		} else if cs.Status == v1alpha1.InstallStatus_RECONCILING {
			ret = v1alpha1.InstallStatus_RECONCILING
			break
		}
	}
	return ret
}

// allObjectHashes returns a map with object hashes of all the objects contained in cmm as the keys.
func allObjectHashes(m string) map[string]bool {
	ret := make(map[string]bool)
	objs, err := object.ParseK8sObjectsFromYAMLManifest(m)
	if err != nil {
		scope.Error(err.Error())
	}
	for _, o := range objs {
		ret[o.Hash()] = true
	}

	return ret
}

// GetClient returns the kubernetes client associated with this HelmReconciler
func (h *HelmReconciler) GetClient() client.Client {
	return h.client
}

func buildInstallTree() {
	// Starting with root, recursively insert each first level child into each node.
	insertChildrenRecursive(name.IstioBaseComponentName, installTree, componentDependencies)
}

func insertChildrenRecursive(componentName name.ComponentName, tree componentTree, children componentNameToListMap) {
	tree[componentName] = make(componentTree)
	for _, child := range children[componentName] {
		insertChildrenRecursive(child, tree[componentName].(componentTree), children)
	}
}
