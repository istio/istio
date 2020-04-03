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

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"istio.io/api/operator/v1alpha1"
	valuesv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/util"
	llog "istio.io/istio/operator/pkg/util/log"
	"istio.io/pkg/log"
)

// HelmReconciler reconciles resources rendered by a set of helm charts.
type HelmReconciler struct {
	client             client.Client
	iop                *valuesv1alpha1.IstioOperator
	pruningDetails     PruningDetails
	opts               *Options
	needUpdateAndPrune bool
	// copy of the last generated manifests.
	manifests name.ManifestMap
}

// Options are options for HelmReconciler.
type Options struct {
	// DryRun executes all actions but does not write anything to the cluster.
	DryRun bool
	// Logger is a logger for user facing output.
	Logger llog.Logger
}

var defaultOptions = &Options{
	Logger: llog.NewDefaultLogger(),
}

// NewHelmReconciler creates a HelmReconciler and returns a ptr to it
func NewHelmReconciler(client client.Client, iop *valuesv1alpha1.IstioOperator, opts *Options) *HelmReconciler {
	if opts == nil {
		opts = defaultOptions
	}
	return &HelmReconciler{
		client:         client,
		iop:            iop,
		pruningDetails: NewIstioPruningDetails(iop),
		opts:           opts,
	}
}

// Reconcile reconciles the associated resources.
func (h *HelmReconciler) Reconcile() error {
	if err := h.beginReconcile(); err != nil {
		return err
	}

	manifestMap, err := h.RenderCharts()
	if err != nil {
		// TODO: this needs to update status to RECONCILING.
		return err
	}

	status := h.processRecursive(manifestMap)

	// Delete any resources not in the manifest but managed by operator.
	var errs util.Errors
	if h.needUpdateAndPrune {
		errs = util.AppendErr(errs, h.Prune(allObjectHashes(manifestMap), false))
	}
	errs = util.AppendErr(errs, h.endReconcile(status))

	return errs.ToError()
}

// beginReconcile updates the status field on the IstioOperator instance before reconciling.
func (h *HelmReconciler) beginReconcile() error {
	isop := &valuesv1alpha1.IstioOperator{}
	namespacedName := types.NamespacedName{
		Name:      h.iop.Name,
		Namespace: h.iop.Namespace,
	}
	if err := h.GetClient().Get(context.TODO(), namespacedName, isop); err != nil {
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

// endReconcile updates the status field on the IstioOperator instance based on the resulting err parameter.
func (h *HelmReconciler) endReconcile(status *v1alpha1.InstallStatus) error {
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

// processRecursive processes the given manifests in an order of dependencies defined in h. Dependencies are a tree,
// where a child must wait for the parent to complete before starting.
func (h *HelmReconciler) processRecursive(manifests ChartManifestsMap) *v1alpha1.InstallStatus {
	deps, dch := getProcessingOrder(manifests)
	componentStatus := make(map[string]*v1alpha1.InstallStatus_VersionStatus)

	// mu protects the shared InstallStatus componentStatus across goroutines
	var mu sync.Mutex
	// wg waits for all manifest processing goroutines to finish
	var wg sync.WaitGroup

	for c, m := range manifests {
		c, m := c, m
		wg.Add(1)
		go func() {
			defer wg.Done()
			cn := name.ComponentName(c)
			if s := dch[cn]; s != nil {
				log.Infof("%s is waiting on dependency...", c)
				<-s
				log.Infof("Dependency for %s has completed, proceeding.", c)
			}

			// Set status when reconciling starts
			status := v1alpha1.InstallStatus_RECONCILING
			mu.Lock()
			if _, ok := componentStatus[c]; !ok {
				componentStatus[c] = &v1alpha1.InstallStatus_VersionStatus{}
				componentStatus[c].Status = status
			}
			mu.Unlock()

			// Process manifests and get the status result
			errString := ""
			if len(m) == 0 {
				status = v1alpha1.InstallStatus_NONE
			} else {
				status = v1alpha1.InstallStatus_HEALTHY
				if cnt, err := h.ProcessManifest(m[0]); err != nil {
					errString = err.Error()
					status = v1alpha1.InstallStatus_ERROR
				} else if cnt == 0 {
					status = v1alpha1.InstallStatus_NONE
				}
			}

			// Update status based on the result
			mu.Lock()
			if status == v1alpha1.InstallStatus_NONE {
				delete(componentStatus, c)
			} else {
				componentStatus[c].Status = status
				if errString != "" {
					componentStatus[c].Error = errString
				}
			}
			mu.Unlock()

			// Signal all the components that depend on us.
			for _, ch := range deps[cn] {
				log.Infof("Unblocking dependency %s.", ch)
				dch[ch] <- struct{}{}
			}
		}()
	}
	wg.Wait()

	// Update overall status
	// - If all components are HEALTHY, overall status is HEALTHY.
	// - If one or more components are RECONCILING and others are HEALTHY, overall status is RECONCILING.
	// - If one or more components are UPDATING and others are HEALTHY, overall status is UPDATING.
	// - If components are a mix of RECONCILING, UPDATING and HEALTHY, overall status is UPDATING.
	// - If any component is in ERROR state, overall status is ERROR.
	overallStatus := v1alpha1.InstallStatus_HEALTHY
	for _, cs := range componentStatus {
		if cs.Status == v1alpha1.InstallStatus_ERROR {
			overallStatus = v1alpha1.InstallStatus_ERROR
			break
		} else if cs.Status == v1alpha1.InstallStatus_UPDATING {
			overallStatus = v1alpha1.InstallStatus_UPDATING
			break
		} else if cs.Status == v1alpha1.InstallStatus_RECONCILING {
			overallStatus = v1alpha1.InstallStatus_RECONCILING
			break
		}
	}

	out := &v1alpha1.InstallStatus{
		Status:          overallStatus,
		ComponentStatus: componentStatus,
	}

	return out
}

// getProcessingOrder returns the order in which the rendered charts should be processed.
func getProcessingOrder(m ChartManifestsMap) (ComponentNameToListMap, DependencyWaitCh) {
	componentNameList := make([]name.ComponentName, 0)
	dependencyWaitCh := make(DependencyWaitCh)
	for c := range m {
		cn := name.ComponentName(c)
		if cn == name.IstioBaseComponentName {
			continue
		}
		componentNameList = append(componentNameList, cn)
		dependencyWaitCh[cn] = make(chan struct{}, 1)
	}
	componentDependencies := ComponentNameToListMap{
		name.IstioBaseComponentName: componentNameList,
	}
	return componentDependencies, dependencyWaitCh
}

// Delete resources associated with the custom resource instance
func (h *HelmReconciler) Delete() error {
	h.needUpdateAndPrune = true
	return h.Prune(nil, true)
}

// allObjectHashes returns a map with object hashes of all the objects contained in cmm as the keys.
func allObjectHashes(cmm ChartManifestsMap) map[string]bool {
	ret := make(map[string]bool)
	for _, mm := range cmm {
		for _, m := range mm {
			objs, err := object.ParseK8sObjectsFromYAMLManifest(m.Content)
			if err != nil {
				log.Error(err.Error())
			}
			for _, o := range objs {
				ret[o.Hash()] = true
			}
		}
	}
	return ret
}

// GetClient returns the kubernetes client associated with this HelmReconciler
func (h *HelmReconciler) GetClient() client.Client {
	return h.client
}
