package workload

import (
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
)

type handlerController interface {
	Cluster() string
	WorkloadInstanceHandler(si *model.WorkloadInstance, event model.Event)
	AppendWorkloadHandler(f func(*model.WorkloadInstance, model.Event)) error
}

const ServiceEntryKey = "ServiceEntryCache"

// RegisterHandlers is a helper function to wire a Kubernetes registry to the ServiceEntry registry. This allows for
// selection of Pod by ServiceEntry and WorkloadEntry by Service. The ServiceEntry cache must have the cache handler
// set elsewhere to avoid registering it multiple times.
func RegisterHandlers(
	kubeCache, wleCache Cache,
	kubeRegistry, seRegistry handlerController,
) {
	if features.EnableServiceEntrySelectPods && kubeRegistry != nil {
		// Cache Pod based WorkloadInstances
		_ = kubeRegistry.AppendWorkloadHandler(kubeCache.WorkloadInstanceHandler)
		// Notify ServiceEntry registry
		kubeCache.SetHandler(ServiceEntryKey, seRegistry.WorkloadInstanceHandler)
	}
	if features.EnableK8SServiceSelectWorkloadEntries && kubeRegistry != nil {
		// Notify kube registry of WorkloadEntry changes
		wleCache.SetHandler(kubeRegistry.Cluster(), kubeRegistry.WorkloadInstanceHandler)
	}
}

func NewCache() *instanceCache {
	return &instanceCache{
		handlers:   map[string]func(si *model.WorkloadInstance, event model.Event){},
		nameCache:  map[string]map[string]*model.WorkloadInstance{},
		ipNetCache: map[string]map[string]*model.WorkloadInstance{},
	}
}

// Cache can be used by controllers to read workload instances from other registries. A registry
// should usually manage its own internal store of workload instances for the cluster/source it concerns.
type Cache interface {
	All() []*model.WorkloadInstance
	ByName(name string, namespace string) *model.WorkloadInstance
	ByIPNetwork(ip, network string) *model.WorkloadInstance
	// ByIP finds a WorkloadInstance by IP assuming it has no network specified Same as `ByIPNetwork("1.1.1.1", "")`.
	ByIP(ip string) *model.WorkloadInstance

	// WorkloadInstanceHandler ingests changes into the cache.
	WorkloadInstanceHandler(si *model.WorkloadInstance, event model.Event)

	// SetHandler will notify other registries when the cache has changes.
	SetHandler(key string, h func(*model.WorkloadInstance, model.Event))
	RemoveHandler(key string)
}

var _ Cache = &instanceCache{}

type instanceCache struct {
	handlers map[string]func(si *model.WorkloadInstance, event model.Event)

	// first key is namespace, inner key is name
	nameCache map[string]map[string]*model.WorkloadInstance
	// first key is network, inner key is ip
	ipNetCache map[string]map[string]*model.WorkloadInstance
}

func (i *instanceCache) SetHandler(key string, h func(*model.WorkloadInstance, model.Event)) {
	i.handlers[key] = h
}

func (i *instanceCache) RemoveHandler(key string) {
	delete(i.handlers, key)
}

func (i *instanceCache) WorkloadInstanceHandler(si *model.WorkloadInstance, event model.Event) {
	// unselectable
	if si.Namespace == "" || len(si.Endpoint.Labels) == 0 {
		return
	}

	// update store
	switch event {
	case model.EventDelete:
		delete(i.nameCache[si.Namespace], si.Name)
		if si.Endpoint != nil {
			delete(i.ipNetCache[si.Endpoint.Network], si.Endpoint.Address)
		}
	default:
		// add or update

		// TODO add a "is redudant" flag/return value to decide whether or not to notify ServiceEntry

		// TODO NetworkIpByName logic - allows handling IP change for an existing workload (probably also need network in there)

		// by name
		if i.nameCache[si.Namespace] == nil {
			i.nameCache[si.Namespace] = map[string]*model.WorkloadInstance{}
		}
		i.nameCache[si.Namespace][si.Name] = si

		// by address
		if si.Endpoint != nil {
			if i.ipNetCache[si.Endpoint.Network] == nil {
				i.nameCache[si.Endpoint.Network] = map[string]*model.WorkloadInstance{}
			}
			i.nameCache[si.Endpoint.Network][si.Endpoint.Address] = si
		}
	}
	// notify handlers
	for _, h := range i.handlers {
		h(si, event)
	}
}

func (i *instanceCache) All() []*model.WorkloadInstance {
	// TODO optimize this
	var out []*model.WorkloadInstance
	for _, instances := range i.nameCache {
		for _, i := range instances {
			out = append(out, i)
		}
	}
	return out
}

func (i *instanceCache) ByName(name, namespace string) *model.WorkloadInstance {
	if nsCache := i.nameCache[namespace]; nsCache != nil {
		return nsCache[name]
	}
	return nil
}

func (i *instanceCache) ByIPNetwork(ip, network string) *model.WorkloadInstance {
	if netCache := i.ipNetCache[network]; netCache != nil {
		return netCache[ip]
	}
	return nil
}

func (i *instanceCache) ByIP(ip string) *model.WorkloadInstance {
	return i.ByIPNetwork(ip, "")
}
