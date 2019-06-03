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
	"fmt"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// HelmReconciler reconciles resources rendered by a set of helm charts for a specific instances of a custom resource,
// or deletes all resources associated with a specific instance of a custom resource.
type HelmReconciler struct {
	client     client.Client
	logger     logr.Logger
	customizer RenderingCustomizer
	instance   runtime.Object
}

var _ LoggerProvider = &HelmReconciler{}
var _ ClientProvider = &HelmReconciler{}

// Factory is a factory for creating HelmReconciler objects using the specified CustomizerFactory.
type Factory struct {
	// CustomizerFactory is a factory for creating the Customizer object for the HelmReconciler.
	CustomizerFactory RenderingCustomizerFactory
}

// New Returns a new HelmReconciler for the custom resource.
// instance is the custom resource to be reconciled/deleted.
// client is the kubernetes client
// logger is the logger
func (f *Factory) New(instance runtime.Object, client client.Client, logger logr.Logger) (*HelmReconciler, error) {
	delegate, err := f.CustomizerFactory.NewCustomizer(instance)
	if err != nil {
		return nil, err
	}
	wrappedcustomizer, err := wrapCustomizer(instance, delegate)
	if err != nil {
		return nil, err
	}
	reconciler := &HelmReconciler{client: client, logger: logger, customizer: wrappedcustomizer, instance: instance}
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
		err = errors.Wrap(err, fmt.Sprintf("error rendering charts"))
		listenerErr := h.customizer.Listener().EndReconcile(h.instance, err)
		if listenerErr != nil {
			h.logger.Error(listenerErr, "unexpected error invoking EndReconcile")
		}
		return err
	}

	// determine processing order
	chartOrder, err := h.customizer.Input().GetProcessingOrder(manifestMap)
	if err != nil {
		err = errors.Wrap(err, fmt.Sprintf("error ordering charts"))
		listenerErr := h.customizer.Listener().EndReconcile(h.instance, err)
		if listenerErr != nil {
			h.logger.Error(listenerErr, "unexpected error invoking EndReconcile")
		}
		return err
	}

	// collect the errors.  from here on, we'll process everything with the assumption that any error is not fatal.
	allErrors := []error{}

	// process the charts
	for _, chartName := range chartOrder {
		chartManifests, ok := manifestMap[chartName]
		if !ok {
			// TODO: log warning about missing chart
			continue
		}
		chartManifests, err := h.customizer.Listener().BeginChart(chartName, chartManifests)
		if err != nil {
			allErrors = append(allErrors, err)
		}
		err = h.processManifests(chartManifests)
		if err != nil {
			allErrors = append(allErrors, err)
		}
		err = h.customizer.Listener().EndChart(chartName)
		if err != nil {
			allErrors = append(allErrors, err)
		}
	}

	// delete any obsolete resources
	err = h.customizer.Listener().BeginPrune(false)
	if err != nil {
		allErrors = append(allErrors, err)
	}
	err = h.prune(false)
	if err != nil {
		allErrors = append(allErrors, err)
	}
	err = h.customizer.Listener().EndPrune()
	if err != nil {
		allErrors = append(allErrors, err)
	}

	// any post processing required after updating
	err = h.customizer.Listener().EndReconcile(h.instance, utilerrors.NewAggregate(allErrors))
	if err != nil {
		allErrors = append(allErrors, err)
	}

	// return any errors
	return utilerrors.NewAggregate(allErrors)
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
	err = h.prune(true)
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
		h.logger.Error(listenerErr, "error calling listener")
	}

	// return any errors
	return err
}

// GetLogger returns the logger associated with this HelmReconciler
func (h *HelmReconciler) GetLogger() logr.Logger {
	return h.logger
}

// GetClient returns the kubernetes client associated with this HelmReconciler
func (h *HelmReconciler) GetClient() client.Client {
	return h.client
}
