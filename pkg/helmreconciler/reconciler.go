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

	"istio.io/operator/pkg/apis/istio/v1alpha2"

	"istio.io/operator/pkg/util"

	"istio.io/pkg/log"

	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"istio.io/operator/pkg/name"
)

// HelmReconciler reconciles resources rendered by a set of helm charts for a specific instances of a custom resource,
// or deletes all resources associated with a specific instance of a custom resource.
type HelmReconciler struct {
	client     client.Client
	customizer RenderingCustomizer
	instance   runtime.Object
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
func (f *Factory) New(instance runtime.Object, client client.Client) (*HelmReconciler, error) {
	delegate, err := f.CustomizerFactory.NewCustomizer(instance)
	if err != nil {
		return nil, err
	}
	wrappedcustomizer, err := wrapCustomizer(instance, delegate)
	if err != nil {
		return nil, err
	}
	reconciler := &HelmReconciler{client: client, customizer: wrappedcustomizer, instance: instance}
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

	status := h.processRecursive(manifestMap)

	// Delete any resources not in the manifest but managed by operator.
	var errs util.Errors
	errs = util.AppendErr(errs, h.customizer.Listener().BeginPrune(false))
	errs = util.AppendErr(errs, h.Prune(false))
	errs = util.AppendErr(errs, h.customizer.Listener().EndPrune())

	errs = util.AppendErr(errs, h.customizer.Listener().EndReconcile(h.instance, status))

	return errs.ToError()
}

// processRecursive processes the given manifests in an order of dependencies defined in h. Dependencies are a tree,
// where a child must wait for the parent to complete before starting.
func (h *HelmReconciler) processRecursive(manifests ChartManifestsMap) *v1alpha2.InstallStatus {
	deps, dch := h.customizer.Input().GetProcessingOrder(manifests)
	out := &v1alpha2.InstallStatus{Status: make(map[string]*v1alpha2.InstallStatus_VersionStatus)}

	var wg sync.WaitGroup
	var mu sync.Mutex
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
			mu.Lock()
			if _, ok := out.Status[c]; !ok {
				out.Status[c] = &v1alpha2.InstallStatus_VersionStatus{}
			}
			mu.Unlock()
			status := v1alpha2.InstallStatus_NONE
			errString := ""
			if len(m) != 0 {
				status = v1alpha2.InstallStatus_HEALTHY
				if err := h.ProcessManifest(m[0]); err != nil {
					errString = err.Error()
					status = v1alpha2.InstallStatus_ERROR
				}
			}
			mu.Lock()
			out.Status[c].Status = status
			if errString != "" {
				out.Status[c].Error = errString
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
func (h *HelmReconciler) GetInstance() runtime.Object {
	return h.instance
}
