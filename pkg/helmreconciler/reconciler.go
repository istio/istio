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
	"sync"

	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"istio.io/api/operator/v1alpha1"
	iop "istio.io/operator/pkg/apis/istio/v1alpha1"
	"istio.io/operator/pkg/name"
	"istio.io/operator/pkg/util"
	"istio.io/pkg/log"
)

// HelmReconciler reconciles resources rendered by a set of helm charts for a specific instances of a custom resource,
// or deletes all resources associated with a specific instance of a custom resource.
type HelmReconciler struct {
	client             client.Client
	customizer         RenderingCustomizer
	instance           *iop.IstioOperator
	needUpdateAndPrune bool
}

// Factory is a factory for creating HelmReconciler objects using the specified CustomizerFactory.
type Factory struct {
	// CustomizerFactory is a factory for creating the Customizer object for the HelmReconciler.
	CustomizerFactory RenderingCustomizerFactory
}

// New Returns a new HelmReconciler for the custom resource.
// instance is the custom resource to be reconciled/deleted.
// client is the kubernetes client
// logger is the logger
func (f *Factory) New(instance *iop.IstioOperator, client client.Client) (*HelmReconciler, error) {
	delegate, err := f.CustomizerFactory.NewCustomizer(instance)
	if err != nil {
		return nil, err
	}
	wrappedcustomizer, err := wrapCustomizer(instance, delegate)
	if err != nil {
		return nil, err
	}
	reconciler := &HelmReconciler{client: client, customizer: wrappedcustomizer, instance: instance, needUpdateAndPrune: true}
	wrappedcustomizer.RegisterReconciler(reconciler)
	return reconciler, nil
}

// wrapCustomizer creates a new internalCustomizer object wrapping the delegate, by inject a LoggingRenderingListener,
// an OwnerReferenceDecorator, and a PruningDetailsDecorator into a CompositeRenderingListener that includes the listener
// from the delegate.  This ensures the HelmReconciler can properly implement pruning, etc.
// instance is the custom resource to be processed by the HelmReconciler
// delegate is the delegate
func wrapCustomizer(instance runtime.Object, delegate RenderingCustomizer) (*SimpleRenderingCustomizer, error) {
	ownerReferenceDecorator, err := NewOwnerReferenceDecorator(instance)
	if err != nil {
		return nil, err
	}
	return &SimpleRenderingCustomizer{
		InputValue:          delegate.Input(),
		PruningDetailsValue: delegate.PruningDetails(),
		ListenerValue: &CompositeRenderingListener{
			Listeners: []RenderingListener{
				&LoggingRenderingListener{Level: 1},
				ownerReferenceDecorator,
				NewPruningMarkingsDecorator(delegate.PruningDetails()),
				delegate.Listener(),
			},
		},
	}, nil
}

// Reconcile the resources associated with the custom resource instance.
func (h *HelmReconciler) Reconcile() error {
	// any processing required before processing the charts
	err := h.customizer.Listener().BeginReconcile(h.instance)
	if err != nil {
		return err
	}

	// render charts
	manifestMap, err := h.renderCharts(h.customizer.Input())
	if err != nil {
		// TODO: this needs to update status to RECONCILING.
		return err
	}

	// handle the defined callbacks to the generated manifests for each subchart chart.
	//for chartName, manifests := range manifestMap {
	//	newManifests, err := h.customizer.Listener().BeginChart(chartName, manifests)
	//	if err != nil {
	//		return err
	//	}
	//	manifestMap[chartName] = newManifests
	//}
	status := h.processRecursive(manifestMap)

	// Delete any resources not in the manifest but managed by operator.
	var errs util.Errors
	if h.needUpdateAndPrune {
		errs = util.AppendErr(errs, h.customizer.Listener().BeginPrune(false))
		errs = util.AppendErr(errs, h.Prune(false))
		errs = util.AppendErr(errs, h.customizer.Listener().EndPrune())
	}
	errs = util.AppendErr(errs, h.customizer.Listener().EndReconcile(h.instance, status))
	return errs.ToError()
}

// processRecursive processes the given manifests in an order of dependencies defined in h. Dependencies are a tree,
// where a child must wait for the parent to complete before starting.
func (h *HelmReconciler) processRecursive(manifests ChartManifestsMap) map[string]*v1alpha1.IstioOperatorSpec_VersionStatus {
	deps, dch := h.customizer.Input().GetProcessingOrder(manifests)
	out := make(map[string]*v1alpha1.IstioOperatorSpec_VersionStatus)

	// mu protects the shared InstallStatus out across goroutines
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
			status := v1alpha1.IstioOperatorSpec_RECONCILING
			mu.Lock()
			if _, ok := out[c]; !ok {
				out[c] = &v1alpha1.IstioOperatorSpec_VersionStatus{}
				out[c].Status = status
			}
			mu.Unlock()

			// Process manifests and get the status result
			errString := ""
			if len(m) == 0 {
				status = v1alpha1.IstioOperatorSpec_NONE
			} else {
				status = v1alpha1.IstioOperatorSpec_HEALTHY
				if cnt, err := h.ProcessManifest(m[0]); err != nil {
					errString = err.Error()
					status = v1alpha1.IstioOperatorSpec_ERROR
				} else if cnt == 0 {
					status = v1alpha1.IstioOperatorSpec_NONE
				}
			}

			// Update status based on the result
			mu.Lock()
			if status == v1alpha1.IstioOperatorSpec_NONE {
				delete(out, c)
			} else {
				out[c].Status = status
				out[c].StatusString = v1alpha1.IstioOperatorSpec_Status_name[int32(status)]
				if errString != "" {
					out[c].Error = errString
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

	return out
}

// Delete resources associated with the custom resource instance
func (h *HelmReconciler) Delete() error {
	h.needUpdateAndPrune = true
	allErrors := []error{}

	// any processing required before processing the charts
	err := h.customizer.Listener().BeginDelete(h.instance)
	if err != nil {
		allErrors = append(allErrors, err)
	}

	err = h.customizer.Listener().BeginPrune(true)
	if err != nil {
		allErrors = append(allErrors, err)
	}
	err = h.Prune(true)
	if err != nil {
		allErrors = append(allErrors, err)
	}
	err = h.customizer.Listener().EndPrune()
	if err != nil {
		allErrors = append(allErrors, err)
	}

	// any post processing required after deleting
	err = utilerrors.NewAggregate(allErrors)
	if listenerErr := h.customizer.Listener().EndDelete(h.instance, err); listenerErr != nil {
		log.Errorf("error calling listener: %s", listenerErr)
	}

	// return any errors
	return err
}

// GetClient returns the kubernetes client associated with this HelmReconciler
func (h *HelmReconciler) GetClient() client.Client {
	return h.client
}

// GetCustomizer returns the customizer associated with this HelmReconciler
func (h *HelmReconciler) GetCustomizer() RenderingCustomizer {
	return h.customizer
}

// GetInstance returns the instance associated with this HelmReconciler
func (h *HelmReconciler) GetInstance() *iop.IstioOperator {
	return h.instance
}

// SetInstance set the instance associated with this HelmReconciler
func (h *HelmReconciler) SetInstance(instance *iop.IstioOperator) {
	h.instance = instance
}

// SetNeedUpdateAndPrune set the needUpdateAndPrune flag associated with this HelmReconciler
func (h *HelmReconciler) SetNeedUpdateAndPrune(u bool) {
	h.needUpdateAndPrune = u
}
