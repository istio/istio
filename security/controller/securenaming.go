package controller

import (
	"time"

	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/labels"
	"k8s.io/client-go/pkg/runtime"
	"k8s.io/client-go/pkg/watch"
	"k8s.io/client-go/tools/cache"
)

// TODO: check whether this resync period is reasonable.
const resyncPeriod = 10 * time.Second

type SecureNamingController struct {
	serviceController *cache.Controller
	serviceIndexer    cache.Indexer
}

func NewSecureNamingController(core v1core.CoreV1Interface) *SecureNamingController {
	si, sc := newServiceIndexerInformer(core)
	return &SecureNamingController{sc, si}
}

func newServiceIndexerInformer(core v1core.CoreV1Interface) (cache.Indexer, *cache.Controller) {
	si := core.Services(api.NamespaceAll)
	return cache.NewIndexerInformer(
		&cache.ListWatch{
			ListFunc: func(options api.ListOptions) (runtime.Object, error) {
				return si.List(v1.ListOptions{})
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return si.Watch(v1.ListOptions{})
			},
		},
		&v1.Service{},
		resyncPeriod,
		cache.ResourceEventHandlerFuncs{},
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
}

func (snc *SecureNamingController) getPodServices(pod *v1.Pod) []*v1.Service {
	allServices := []*v1.Service{}
	cache.ListAllByNamespace(snc.serviceIndexer, pod.Namespace, labels.Everything(), func(m interface{}) {
		allServices = append(allServices, m.(*v1.Service))
	})

	services := []*v1.Service{}
	for _, service := range allServices {
		if service.Spec.Selector == nil {
			continue
		}
		selector := labels.Set(service.Spec.Selector).AsSelectorPreValidated()
		if selector.Matches(labels.Set(pod.Labels)) {
			services = append(services, service)
		}
	}
	return services
}
