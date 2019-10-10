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

package istiocontrolplane

import (
	"context"
	"fmt"
	"time"

	"istio.io/pkg/log"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/helmreconciler"
)

const (
	// ChartOwnerKey is the annotation key used to store the name of the chart that created the resource
	ChartOwnerKey = MetadataNamespace + "/chart-owner"

	finalizerRemovalBackoffSteps    = 10
	finalizerRemovalBackoffDuration = 6 * time.Second
	finalizerRemovalBackoffFactor   = 1.1
)

// IstioRenderingListener is a RenderingListener specific to IstioControlPlane resources
type IstioRenderingListener struct {
	*helmreconciler.CompositeRenderingListener
}

// IstioStatusUpdater is a RenderingListener that updates the status field on the IstioControlPlane
// instance based on the results of the Reconcile operation.
type IstioStatusUpdater struct {
	*helmreconciler.DefaultRenderingListener
	instance   *v1alpha2.IstioControlPlane
	reconciler *helmreconciler.HelmReconciler
}

// NewIstioRenderingListener returns a new IstioRenderingListener, which is a composite that includes IstioStatusUpdater
// and IstioChartCustomizerListener.
func NewIstioRenderingListener(instance *v1alpha2.IstioControlPlane) *IstioRenderingListener {
	return &IstioRenderingListener{
		&helmreconciler.CompositeRenderingListener{
			Listeners: []helmreconciler.RenderingListener{
				NewChartCustomizerListener(),
				NewIstioStatusUpdater(instance),
			},
		},
	}
}

// NewIstioStatusUpdater returns a new IstioStatusUpdater instance for the specified IstioControlPlane
func NewIstioStatusUpdater(instance *v1alpha2.IstioControlPlane) helmreconciler.RenderingListener {
	return &IstioStatusUpdater{
		DefaultRenderingListener: &helmreconciler.DefaultRenderingListener{},
		instance:                 instance,
	}
}

// EndReconcile updates the status field on the IstioControlPlane instance based on the resulting err parameter.
func (u *IstioStatusUpdater) EndReconcile(_ runtime.Object, status *v1alpha2.InstallStatus) error {
	u.instance.Status = status
	return u.reconciler.GetClient().Status().Update(context.TODO(), u.instance)
}

// RegisterReconciler registers the HelmReconciler with this object
func (u *IstioStatusUpdater) RegisterReconciler(reconciler *helmreconciler.HelmReconciler) {
	u.reconciler = reconciler
}

// IstioChartCustomizerListener provides ChartCustomizer objects specific to IstioControlPlane resources.
type IstioChartCustomizerListener struct {
	*helmreconciler.DefaultChartCustomizerListener
}

var _ helmreconciler.RenderingListener = &IstioChartCustomizerListener{}
var _ helmreconciler.ReconcilerListener = &IstioChartCustomizerListener{}

// NewChartCustomizerListener returns a new IstioChartCustomizerListener
func NewChartCustomizerListener() *IstioChartCustomizerListener {
	listener := &IstioChartCustomizerListener{
		DefaultChartCustomizerListener: helmreconciler.NewDefaultChartCustomizerListener(ChartOwnerKey),
	}
	listener.DefaultChartCustomizerListener.ChartCustomizerFactory = &IstioChartCustomizerFactory{}
	return listener
}

// IstioChartCustomizerFactory creates ChartCustomizer objects specific to IstioControlPlane resources.
type IstioChartCustomizerFactory struct {
	*helmreconciler.DefaultChartCustomizerFactory
}

var _ helmreconciler.ChartCustomizerFactory = &IstioChartCustomizerFactory{}

// NewChartCustomizer returns a new ChartCustomizer for the specific chart.
// Currently, an IstioDefaultChartCustomizer is returned for all charts except: kiali
func (f *IstioChartCustomizerFactory) NewChartCustomizer(chartName string) helmreconciler.ChartCustomizer {
	switch chartName {
	case "istio/charts/kiali":
		return NewKialiChartCustomizer(chartName, f.DefaultChartCustomizerFactory.ChartAnnotationKey)
	default:
		return NewIstioDefaultChartCustomizer(chartName, f.DefaultChartCustomizerFactory.ChartAnnotationKey)
	}
}

// IstioDefaultChartCustomizer represents the default ChartCustomizer for IstioControlPlane charts.
type IstioDefaultChartCustomizer struct {
	*helmreconciler.DefaultChartCustomizer
}

var _ helmreconciler.ChartCustomizer = &IstioDefaultChartCustomizer{}

// NewIstioDefaultChartCustomizer creates a new IstioDefaultChartCustomizer
func NewIstioDefaultChartCustomizer(chartName, chartAnnotationKey string) *IstioDefaultChartCustomizer {
	return &IstioDefaultChartCustomizer{
		DefaultChartCustomizer: helmreconciler.NewDefaultChartCustomizer(chartName, chartAnnotationKey),
	}
}

// EndChart waits for any deployments or stateful sets that were created to become ready
func (c *IstioDefaultChartCustomizer) EndChart(chartName string) error {
	// ignore any errors.  things should settle out
	c.waitForResources()
	return nil
}

func (c *IstioDefaultChartCustomizer) waitForResources() {
	if statefulSets, ok := c.NewResourcesByKind["StatefulSet"]; ok {
		for _, statefulSet := range statefulSets {
			c.waitForDeployment(statefulSet)
		}
	}
	if deployments, ok := c.NewResourcesByKind["Deployment"]; ok {
		for _, deployment := range deployments {
			c.waitForDeployment(deployment)
		}
	}
	if daemonSets, ok := c.NewResourcesByKind["DaemonSet"]; ok {
		for _, daemonSet := range daemonSets {
			c.waitForDeployment(daemonSet)
		}
	}
	if services, ok := c.NewResourcesByKind["Service"]; ok {
		for _, service := range services {
			c.waitForService(service)
		}
	}
}

func (c *IstioDefaultChartCustomizer) serviceReady(svc *corev1.Service) bool {
	// ExternalName Services are external to cluster so they should not be checked
	if svc.Spec.Type == corev1.ServiceTypeExternalName {
		return true
	}
	// Check if services except the headless services have the IP set
	if svc.Spec.ClusterIP != corev1.ClusterIPNone && svc.Spec.ClusterIP == "" {
		log.Info(fmt.Sprintf("Service is not ready: %s/%s", svc.GetNamespace(), svc.GetName()))
		return false
	}
	// Check if the service has a LoadBalancer with an Ingress ready
	if svc.Spec.Type == corev1.ServiceTypeLoadBalancer && svc.Status.LoadBalancer.Ingress == nil {
		log.Info(fmt.Sprintf("Service is not ready: %s/%s", svc.GetNamespace(), svc.GetName()))
		return false
	}
	return true
}

func (c *IstioDefaultChartCustomizer) waitForService(object runtime.Object) {
	gvk := object.GetObjectKind().GroupVersionKind()
	objectAccessor, err := meta.Accessor(object)
	if err != nil {
		log.Error(fmt.Sprintf("could not get object accessor for %s", gvk.Kind))
		return
	}
	name := objectAccessor.GetName()
	service, ok := object.(*corev1.Service)
	if ok {
		log.Infof("waiting for service to become ready; %s", name)
		err = wait.ExponentialBackoff(wait.Backoff{
			Duration: finalizerRemovalBackoffDuration,
			Steps:    finalizerRemovalBackoffSteps,
			Factor:   finalizerRemovalBackoffFactor,
		}, func() (bool, error) {
			return c.serviceReady(service), nil
		})
		if err != nil {
			log.Errorf("service failed to become ready in a timely manner: %s", name)
		}
	}
}

// XXX: configure wait period
func (c *IstioDefaultChartCustomizer) waitForDeployment(object runtime.Object) {
	gvk := object.GetObjectKind().GroupVersionKind()
	objectAccessor, err := meta.Accessor(object)
	if err != nil {
		log.Error(fmt.Sprintf("could not get object accessor for %s", gvk.Kind))
		return
	}
	name := objectAccessor.GetName()
	namespace := objectAccessor.GetNamespace()
	deployment := &unstructured.Unstructured{}
	deployment.SetGroupVersionKind(gvk)
	// wait for deployment replicas >= 1
	log.Infof("waiting for deployment to become ready: %s, %s", gvk.Kind, name)
	err = wait.ExponentialBackoff(wait.Backoff{
		Duration: finalizerRemovalBackoffDuration,
		Steps:    finalizerRemovalBackoffSteps,
		Factor:   finalizerRemovalBackoffFactor,
	}, func() (bool, error) {
		err := c.Reconciler.GetClient().Get(context.TODO(), client.ObjectKey{Namespace: namespace, Name: name}, deployment)
		if err == nil {
			val, _, _ := unstructured.NestedInt64(deployment.UnstructuredContent(), "status", "readyReplicas")
			return val > 0, nil
		} else if errors.IsNotFound(err) {
			log.Errorf("attempting to wait on unknown deployment: %s, %s", gvk.Kind, name)
			return true, nil
		}
		log.Errorf("unexpected error occurred waiting for deployment to become ready: %s, %s: %s", gvk.Kind, name, err)
		return false, err
	})
	if err != nil {
		log.Errorf("deployment failed to become ready in a timely manner: %s, %s", gvk.Kind, name)
	}
}

// KialiChartCustomizer is a ChartCustomizer for the kiali chart
type KialiChartCustomizer struct {
	*IstioDefaultChartCustomizer
}

var _ helmreconciler.ChartCustomizer = &KialiChartCustomizer{}

// NewKialiChartCustomizer creates a new KialiChartCustomizer
func NewKialiChartCustomizer(chartName, chartAnnotationKey string) *KialiChartCustomizer {
	return &KialiChartCustomizer{
		IstioDefaultChartCustomizer: NewIstioDefaultChartCustomizer(chartName, chartAnnotationKey),
	}
}

// BeginResource invokes the default BeginResource behavior for all resources and patches the grafana and jaeger URLs
// in the "kiali" ConfigMap with the actual installed URLs.  (TODO)
func (c *KialiChartCustomizer) BeginResource(obj runtime.Object) (runtime.Object, error) {
	var err error
	if obj, err = c.IstioDefaultChartCustomizer.BeginResource(obj); err != nil {
		return obj, err
	}
	switch obj.GetObjectKind().GroupVersionKind().Kind {
	case "ConfigMap":
		if obj, err = c.patchKialiConfigMap(obj); err != nil {
			return obj, err
		}
	}
	return obj, err
}

func (c *KialiChartCustomizer) patchKialiConfigMap(obj runtime.Object) (runtime.Object, error) {
	// XXX: do we even need to check this?
	if objAccessor, err := meta.Accessor(obj); err != nil || objAccessor.GetName() != "kiali" {
		return obj, err
	}
	switch configMap := obj.(type) {
	case *corev1.ConfigMap:
		// TODO: patch jaeger and grafana urls
		configMap.GroupVersionKind()
	case *unstructured.Unstructured:
		// TODO: patch jaeger and grafana urls
		configMap.GroupVersionKind()
	}
	return obj, nil
}
