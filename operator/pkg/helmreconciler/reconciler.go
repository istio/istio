// Copyright Istio Authors
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
	"os"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"istio.io/api/label"
	"istio.io/api/operator/v1alpha1"
	valuesv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/operator/pkg/util/progress"
	"istio.io/pkg/version"
)

// HelmReconciler reconciles resources rendered by a set of helm charts.
type HelmReconciler struct {
	client     client.Client
	restConfig *rest.Config
	clientSet  *kubernetes.Clientset
	iop        *valuesv1alpha1.IstioOperator
	opts       *Options
	// copy of the last generated manifests.
	manifests name.ManifestMap
	// dependencyWaitCh is a map of signaling channels. A parent with children ch1...chN will signal
	// dependencyWaitCh[ch1]...dependencyWaitCh[chN] when it's completely installed.
	dependencyWaitCh map[name.ComponentName]chan struct{}
}

// Options are options for HelmReconciler.
type Options struct {
	// DryRun executes all actions but does not write anything to the cluster.
	DryRun bool
	// Log is a console logger for user visible CLI output.
	Log clog.Logger
	// Wait determines if we will wait for resources to be fully applied. Only applies to components that have no
	// dependencies.
	Wait bool
	// WaitTimeout controls the amount of time to wait for resources in a component to become ready before giving up.
	WaitTimeout time.Duration
	// Log tracks the installation progress for all components.
	ProgressLog *progress.Log
	// Force ignores validation errors
	Force bool
}

var defaultOptions = &Options{
	Log:         clog.NewDefaultLogger(),
	ProgressLog: progress.NewLog(),
}

// NewHelmReconciler creates a HelmReconciler and returns a ptr to it
func NewHelmReconciler(client client.Client, restConfig *rest.Config, iop *valuesv1alpha1.IstioOperator, opts *Options) (*HelmReconciler, error) {
	if opts == nil {
		opts = defaultOptions
	}
	if opts.ProgressLog == nil {
		opts.ProgressLog = progress.NewLog()
	}
	if waitForResourcesTimeoutStr, found := os.LookupEnv("WAIT_FOR_RESOURCES_TIMEOUT"); found {
		if waitForResourcesTimeout, err := time.ParseDuration(waitForResourcesTimeoutStr); err == nil {
			opts.WaitTimeout = waitForResourcesTimeout
		} else {
			scope.Warnf("invalid env variable value: %s for 'WAIT_FOR_RESOURCES_TIMEOUT'! falling back to default value...", waitForResourcesTimeoutStr)
			// fallback to default wait resource timeout
			opts.WaitTimeout = defaultWaitResourceTimeout
		}
	} else {
		// fallback to default wait resource timeout
		opts.WaitTimeout = defaultWaitResourceTimeout
	}
	if iop == nil {
		// allows controller code to function for cases where IOP is not provided (e.g. operator remove).
		iop = &valuesv1alpha1.IstioOperator{}
		iop.Spec = &v1alpha1.IstioOperatorSpec{}
	}
	if operatorRevision, found := os.LookupEnv("REVISION"); found {
		iop.Spec.Revision = operatorRevision
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
		client:           client,
		restConfig:       restConfig,
		clientSet:        cs,
		iop:              iop,
		opts:             opts,
		dependencyWaitCh: initDependencies(),
	}, nil
}

// initDependencies initializes the dependencies channel tree.
func initDependencies() map[name.ComponentName]chan struct{} {
	ret := make(map[name.ComponentName]chan struct{})
	for _, parent := range ComponentDependencies {
		for _, child := range parent {
			ret[child] = make(chan struct{}, 1)
		}
	}
	return ret
}

// Reconcile reconciles the associated resources.
func (h *HelmReconciler) Reconcile() (*v1alpha1.InstallStatus, error) {
	manifestMap, err := h.RenderCharts()
	if err != nil {
		return nil, err
	}

	status := h.processRecursive(manifestMap)

	h.opts.ProgressLog.SetState(progress.StatePruning)
	pruneErr := h.Prune(manifestMap, false)
	return status, pruneErr
}

// processRecursive processes the given manifests in an order of dependencies defined in h. Dependencies are a tree,
// where a child must wait for the parent to complete before starting.
func (h *HelmReconciler) processRecursive(manifests name.ManifestMap) *v1alpha1.InstallStatus {
	componentStatus := make(map[string]*v1alpha1.InstallStatus_VersionStatus)

	// mu protects the shared InstallStatus componentStatus across goroutines
	var mu sync.Mutex
	// wg waits for all manifest processing goroutines to finish
	var wg sync.WaitGroup

	for c, ms := range manifests {
		c, ms := c, ms
		wg.Add(1)
		go func() {
			var processedObjs object.K8sObjects
			var deployedObjects int
			defer wg.Done()
			if s := h.dependencyWaitCh[c]; s != nil {
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
			if len(ms) != 0 {
				m := name.Manifest{
					Name:    c,
					Content: name.MergeManifestSlices(ms),
				}
				processedObjs, deployedObjects, err = h.ApplyManifest(m)
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
			for _, ch := range ComponentDependencies[c] {
				scope.Infof("Unblocking dependency %s.", ch)
				h.dependencyWaitCh[ch] <- struct{}{}
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
	iop := h.iop
	if iop.Spec.Revision == "" {
		return h.Prune(nil, true)
	}
	// Delete IOP with revision:
	// for this case we update the status field to pending if there are still proxies pointing to this revision
	// and we do not prune shared resources, same effect as `istioctl uninstall --revision foo` command.
	status, err := h.PruneControlPlaneByRevisionWithController(iop.Spec.Namespace, iop.Spec.Revision)
	if err != nil {
		return err
	}
	if err := h.SetStatusComplete(status); err != nil {
		return err
	}
	return nil
}

// DeleteAll deletes all Istio resources in the cluster.
func (h *HelmReconciler) DeleteAll() error {
	manifestMap := name.ManifestMap{}
	for _, c := range name.AllComponentNames {
		manifestMap[c] = nil
	}
	return h.Prune(manifestMap, true)
}

// SetStatusBegin updates the status field on the IstioOperator instance before reconciling.
func (h *HelmReconciler) SetStatusBegin() error {
	isop := &valuesv1alpha1.IstioOperator{}
	namespacedName := types.NamespacedName{
		Name:      h.iop.Name,
		Namespace: h.iop.Namespace,
	}
	if err := h.getClient().Get(context.TODO(), namespacedName, isop); err != nil {
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
	return h.getClient().Status().Update(context.TODO(), isop)
}

// SetStatusComplete updates the status field on the IstioOperator instance based on the resulting err parameter.
func (h *HelmReconciler) SetStatusComplete(status *v1alpha1.InstallStatus) error {
	iop := &valuesv1alpha1.IstioOperator{}
	namespacedName := types.NamespacedName{
		Name:      h.iop.Name,
		Namespace: h.iop.Namespace,
	}
	if err := h.getClient().Get(context.TODO(), namespacedName, iop); err != nil {
		return fmt.Errorf("failed to get IstioOperator before updating status due to %v", err)
	}
	iop.Status = status
	return h.getClient().Status().Update(context.TODO(), iop)
}

// setStatus sets the status for the component with the given name, which is a key in the given map.
// If the status is InstallStatus_NONE, the component name is deleted from the map.
// Otherwise, if the map key/value is missing, one is created.
func setStatus(s map[string]*v1alpha1.InstallStatus_VersionStatus, componentName name.ComponentName, status v1alpha1.InstallStatus_Status, err error) {
	cn := string(componentName)
	if status == v1alpha1.InstallStatus_NONE {
		delete(s, cn)
		return
	}
	if _, ok := s[cn]; !ok {
		s[cn] = &v1alpha1.InstallStatus_VersionStatus{}
	}
	s[cn].Status = status
	if err != nil {
		s[cn].Error = err.Error()
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

// getCoreOwnerLabels returns a map of labels for associating installation resources. This is the common
// labels shared between all resources; see getOwnerLabels to get labels per-component labels
func (h *HelmReconciler) getCoreOwnerLabels() (map[string]string, error) {
	crName, err := h.getCRName()
	if err != nil {
		return nil, err
	}
	crNamespace, err := h.getCRNamespace()
	if err != nil {
		return nil, err
	}
	labels := make(map[string]string)

	labels[operatorLabelStr] = operatorReconcileStr
	if crName != "" {
		labels[OwningResourceName] = crName
	}
	if crNamespace != "" {
		labels[OwningResourceNamespace] = crNamespace
	}
	labels[istioVersionLabelStr] = version.Info.Version

	return labels, nil
}

func (h *HelmReconciler) addComponentLabels(coreLabels map[string]string, componentName string) map[string]string {
	labels := map[string]string{}
	for k, v := range coreLabels {
		labels[k] = v
	}
	revision := ""
	if h.iop != nil {
		revision = h.iop.Spec.Revision
	}
	if revision == "" {
		revision = "default"
	}
	labels[label.IstioRev] = revision

	labels[IstioComponentLabelStr] = componentName

	return labels
}

// getOwnerLabels returns a map of labels for the given component name, revision and owning CR resource name.
func (h *HelmReconciler) getOwnerLabels(componentName string) (map[string]string, error) {
	labels, err := h.getCoreOwnerLabels()
	if err != nil {
		return nil, err
	}

	return h.addComponentLabels(labels, componentName), nil
}

// applyLabelsAndAnnotations applies owner labels and annotations to the object.
func (h *HelmReconciler) applyLabelsAndAnnotations(obj runtime.Object, componentName string) error {
	labels, err := h.getOwnerLabels(componentName)
	if err != nil {
		return err
	}

	for k, v := range labels {
		err := util.SetLabel(obj, k, v)
		if err != nil {
			return err
		}
	}
	return nil
}

// getCRName returns the name of the CR associated with h.
func (h *HelmReconciler) getCRName() (string, error) {
	if h.iop == nil {
		return "", nil
	}
	objAccessor, err := meta.Accessor(h.iop)
	if err != nil {
		return "", err
	}
	return objAccessor.GetName(), nil
}

// getCRHash returns the cluster unique hash of the CR associated with h.
func (h *HelmReconciler) getCRHash(componentName string) (string, error) {
	crName, err := h.getCRName()
	if err != nil {
		return "", err
	}
	crNamespace, err := h.getCRNamespace()
	if err != nil {
		return "", err
	}
	var host string
	if h.restConfig != nil {
		host = h.restConfig.Host
	}
	return strings.Join([]string{crName, crNamespace, componentName, host}, "-"), nil
}

// getCRNamespace returns the namespace of the CR associated with h.
func (h *HelmReconciler) getCRNamespace() (string, error) {
	if h.iop == nil {
		return "", nil
	}
	objAccessor, err := meta.Accessor(h.iop)
	if err != nil {
		return "", err
	}
	return objAccessor.GetNamespace(), nil
}

// getClient returns the kubernetes client associated with this HelmReconciler
func (h *HelmReconciler) getClient() client.Client {
	return h.client
}
