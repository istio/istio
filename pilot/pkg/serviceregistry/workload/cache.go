package workload

import "istio.io/istio/pilot/pkg/model"

type Aggregate interface {
	// For external readers
	Kube() Cache
	WorkloadEntry() Cache

	// Incoming changes
	WorkloadInstanceHandler(si *model.WorkloadInstance, event model.Event)
	WorkloadEntryHandler(si *model.WorkloadInstance, event model.Event)

	// Outgoing changes
	AppendHandler(key string, h func(*model.WorkloadInstance, model.Event))
	RemoveWorkloadHandler(key string)
}

const ServiceEntryKey = "ServiceEntryCache"

var _ Aggregate = &instanceAgg{}

func NewAggregate() Aggregate {
	return &instanceAgg{
		wleCache:  newCache(),
		kubeCache: newCache(),
		handlers:  map[string]func(si *model.WorkloadInstance, event model.Event){},
	}
}

type instanceAgg struct {
	kubeCache *instanceCache
	wleCache  *instanceCache
	handlers  map[string]func(si *model.WorkloadInstance, event model.Event)
}

// WorkloadInstanceHandler should be used for named registries. Other names registries are not notified of changes
// triggered by this handler.
func (i *instanceAgg) WorkloadInstanceHandler(si *model.WorkloadInstance, event model.Event) {
	// store
	i.kubeCache.WorkloadInstanceHandler(si, event)
	// notify
	for k, h := range i.handlers {
		if k == ServiceEntryKey {
			h(si, event)
			break
		}
	}
}

// WorkloadEntryHandler handles to WorkloadEntry. All named registries will be notified of the update.
func (i *instanceAgg) WorkloadEntryHandler(si *model.WorkloadInstance, event model.Event) {
	// store
	i.wleCache.WorkloadInstanceHandler(si, event)
	// notify
	for k, h := range i.handlers {
		if k == ServiceEntryKey {
			continue
		}
		h(si, event)
	}
}

func (i *instanceAgg) AppendHandler(key string, h func(*model.WorkloadInstance, model.Event)) {
	i.handlers[key] = h
}

func (i *instanceAgg) RemoveWorkloadHandler(key string) {
	delete(i.handlers, key)
}

func (i *instanceAgg) WorkloadEntry() Cache {
	return i.wleCache
}

func (i *instanceAgg) Kube() Cache {
	return i.kubeCache
}

func newCache() *instanceCache {
	return &instanceCache{
		nameCache:  map[string]map[string]*model.WorkloadInstance{},
		ipNetCache: map[string]map[string]*model.WorkloadInstance{},
	}
}

// Cache can be used by controllers to read workload instances from other registries. A registry
// should usually manage its own internal store of workload instances for the cluster/source it concerns.
type Cache interface {
	All() []*model.WorkloadInstance
	Get(name string, namespace string) *model.WorkloadInstance
	ByIPNetwork(ip, network string) *model.WorkloadInstance
	// ByIP finds a WorkloadInstance by IP assuming it has no network specified Same as `ByIPNetwork("1.1.1.1", "")`.
	ByIP(ip string) *model.WorkloadInstance
}

var _ Cache = &instanceCache{}

type instanceCache struct {
	// first key is namespace, inner key is name
	nameCache map[string]map[string]*model.WorkloadInstance
	// first key is network, inner key is ip
	ipNetCache map[string]map[string]*model.WorkloadInstance
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

func (i *instanceCache) WorkloadInstanceHandler(si *model.WorkloadInstance, event model.Event) {
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
}

func (i *instanceCache) Get(name, namespace string) *model.WorkloadInstance {
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
