package controller

import (
	"github.com/hashicorp/go-multierror"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/filter"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

// On namespace membership change, request a full xDS update since services need to be recomputed
func (c *Controller) initDiscoveryNamespaceHandlers(kubeClient kubelib.Client, endpointMode EndpointMode) {
	otype := "Namespaces"
	c.nsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			incrementEvent(otype, "add")
			ns := obj.(*v1.Namespace)
			if ns.Labels[filter.PilotDiscoveryLabelName] == filter.PilotDiscoveryLabelValue {
				c.queue.Push(func() error {
					// labeled namespace was newly created, issue creates for all services, pods, and endpoints in namespace
					if err := c.SyncAll(); err != nil {
						return err
					}
					return nil
				})
			}
		},
		UpdateFunc: func(old, new interface{}) {
			incrementEvent(otype, "update")
			oldNs := old.(*v1.Namespace)
			newNs := new.(*v1.Namespace)
			if oldNs.Labels[filter.PilotDiscoveryLabelName] != newNs.Labels[filter.PilotDiscoveryLabelName] {
				var handleFunc func() error
				if oldNs.Labels[filter.PilotDiscoveryLabelName] == filter.PilotDiscoveryLabelValue {
					// namespace was delabeled, issue deletes for all services, pods, and endpoints in namespace
					handleFunc = func() error {
						c.handleDelabledNamespace(kubeClient, endpointMode, newNs.Name)
						return nil
					}
				} else {
					// namespace was newly labeled, issue creates for all services, pods, and endpoints in namespace
					handleFunc = func() error {
						return c.SyncAll()
					}
				}
				c.queue.Push(handleFunc)
			}
		},
		// no need to handle namespace deletion since all objects within the namespace will trigger delete events
	})
}

// issue delete events for all services, pods, and endpoints in the delabled namespace
func (c *Controller) handleDelabledNamespace(kubeClient kubelib.Client, endpointMode EndpointMode, ns string) {
	var errs *multierror.Error

	// for each resource type, issue delete events for objects in the delabled namespace

	services, err := kubeClient.KubeInformer().Core().V1().Services().Lister().Services(ns).List(labels.Everything())
	if err != nil {
		log.Errorf("error listing services: %v", err)
		return
	}
	for _, svc := range services {
		errs = multierror.Append(errs, c.onServiceEvent(svc, model.EventDelete))
	}

	pods, err := kubeClient.KubeInformer().Core().V1().Pods().Lister().Pods(ns).List(labels.Everything())
	if err != nil {
		log.Errorf("error listing pods: %v", err)
		return
	}
	for _, pod := range pods {
		errs = multierror.Append(errs, c.pods.onEvent(pod, model.EventDelete))
	}

	switch endpointMode {
	case EndpointsOnly:
		endpoints, err := kubeClient.KubeInformer().Core().V1().Endpoints().Lister().Endpoints(ns).List(labels.Everything())
		if err != nil {
			log.Errorf("error listing endpoints: %v", err)
			return
		}
		for _, ep := range endpoints {
			errs = multierror.Append(errs, c.endpoints.onEvent(ep, model.EventDelete))
		}
	case EndpointSliceOnly:
		endpointSlices, err := kubeClient.KubeInformer().Discovery().V1beta1().EndpointSlices().Lister().EndpointSlices(ns).List(labels.Everything())
		if err != nil {
			log.Errorf("error listing endpoint slices: %v", err)
			return
		}
		for _, ep := range endpointSlices {
			errs = multierror.Append(errs, c.endpoints.onEvent(ep, model.EventDelete))
		}
	}

	if err := multierror.Flatten(errs.ErrorOrNil()); err != nil {
		log.Errorf("one or more errors while handling delabeled discovery namespace: %v", err)
	}
}
