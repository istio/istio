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

package controller

import (
	"fmt"
	"istio.io/istio/pkg/config/labels"
	"istio.io/pkg/log"
	"k8s.io/api/core/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes/scheme"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/mesh"
	kubelib "istio.io/istio/pkg/kube"
)

const (
	defaultFakeDomainSuffix = "company.com"
)

// FakeXdsUpdater is used to test the registry.
type FakeXdsUpdater struct {
	// Events tracks notifications received by the updater
	Events chan FakeXdsEvent
}

var _ model.XDSUpdater = &FakeXdsUpdater{}

func (fx *FakeXdsUpdater) ConfigUpdate(*model.PushRequest) {
	select {
	case fx.Events <- FakeXdsEvent{Type: "xds"}:
	default:
	}
}

func (fx *FakeXdsUpdater) ProxyUpdate(_, _ string) {
	select {
	case fx.Events <- FakeXdsEvent{Type: "proxy"}:
	default:
	}
}

// FakeXdsEvent is used to watch XdsEvents
type FakeXdsEvent struct {
	// Type of the event
	Type string

	// The id of the event
	ID string

	// The endpoints associated with an EDS push if any
	Endpoints []*model.IstioEndpoint
}

// NewFakeXDS creates a XdsUpdater reporting events via a channel.
func NewFakeXDS() *FakeXdsUpdater {
	return &FakeXdsUpdater{
		Events: make(chan FakeXdsEvent, 100),
	}
}

func (fx *FakeXdsUpdater) EDSUpdate(_, hostname string, _ string, entry []*model.IstioEndpoint) {
	if len(entry) > 0 {
		select {
		case fx.Events <- FakeXdsEvent{Type: "eds", ID: hostname, Endpoints: entry}:
		default:
		}
	}
}

func (fx *FakeXdsUpdater) EDSCacheUpdate(_, _, _ string, entry []*model.IstioEndpoint) {
}

// SvcUpdate is called when a service port mapping definition is updated.
// This interface is WIP - labels, annotations and other changes to service may be
// updated to force a EDS and CDS recomputation and incremental push, as it doesn't affect
// LDS/RDS.
func (fx *FakeXdsUpdater) SvcUpdate(_, hostname string, _ string, _ model.Event) {
	select {
	case fx.Events <- FakeXdsEvent{Type: "service", ID: hostname}:
	default:
	}
}

func (fx *FakeXdsUpdater) Wait(et string) *FakeXdsEvent {
	for {
		select {
		case e := <-fx.Events:
			if e.Type == et {
				return &e
			}
			continue
		case <-time.After(5 * time.Second):
			return nil
		}
	}
}

// Clear any pending event
func (fx *FakeXdsUpdater) Clear() {
	wait := true
	for wait {
		select {
		case <-fx.Events:
		default:
			wait = false
		}
	}
}

type FakeControllerOptions struct {
	Objects           []runtime.Object
	ObjectString      string
	NetworksWatcher   mesh.NetworksWatcher
	ServiceHandler    func(service *model.Service, event model.Event)
	InstanceHandler   func(instance *model.ServiceInstance, event model.Event)
	Mode              EndpointMode
	ClusterID         string
	WatchedNamespaces string
	DomainSuffix      string
	XDSUpdater        model.XDSUpdater
}

type FakeController struct {
	*Controller
}

func NewFakeControllerWithOptions(opts FakeControllerOptions) (*FakeController, *FakeXdsUpdater) {
	xdsUpdater := opts.XDSUpdater
	if xdsUpdater == nil {
		xdsUpdater = NewFakeXDS()
	}

	domainSuffix := defaultFakeDomainSuffix
	if opts.DomainSuffix != "" {
		domainSuffix = opts.DomainSuffix
	}
	clients := kubelib.NewFakeClient(getKubernetesObjects(opts)...)
	options := Options{
		WatchedNamespaces: opts.WatchedNamespaces, // default is all namespaces
		ResyncPeriod:      1 * time.Second,
		DomainSuffix:      domainSuffix,
		XDSUpdater:        xdsUpdater,
		Metrics:           &model.Environment{},
		NetworksWatcher:   opts.NetworksWatcher,
		EndpointMode:      opts.Mode,
		ClusterID:         opts.ClusterID,
	}
	c := NewController(clients, options)
	if opts.InstanceHandler != nil {
		_ = c.AppendInstanceHandler(opts.InstanceHandler)
	}
	if opts.ServiceHandler != nil {
		_ = c.AppendServiceHandler(opts.ServiceHandler)
	}
	c.stop = make(chan struct{})
	// Run in initiation to prevent calling each test
	// TODO: fix it, so we can remove `stop` channel
	go c.Run(c.stop)
	clients.RunAndWait(c.stop)
	// Wait for the caches to sync, otherwise we may hit race conditions where events are dropped
	cache.WaitForCacheSync(c.stop, c.HasSynced)

	var fx *FakeXdsUpdater
	if x, ok := xdsUpdater.(*FakeXdsUpdater); ok {
		fx = x
	}
	return &FakeController{c}, fx
}

func getKubernetesObjects(opts FakeControllerOptions) []runtime.Object {
	if len(opts.Objects) > 0 {
		return opts.Objects
	}

	objects := make([]runtime.Object, 0)
	if len(opts.ObjectString) > 0 {
		decode := scheme.Codecs.UniversalDeserializer().Decode
		objectStrs := strings.Split(opts.ObjectString, "---")
		for _, s := range objectStrs {
			if len(strings.TrimSpace(s)) == 0 {
				continue
			}
			o, _, err := decode([]byte(s), nil, nil)
			if err != nil {
				log.Warn("failed decoding kubernetes object for fake controller")
				continue
			}
			objects = append(objects, o)
		}
	}

	return objects
}

type FakeServiceOpts struct {
	Name         string
	Namespace    string
	PodIPs       []string
	PodLabels    labels.Instance
	ServicePorts []v1.ServicePort
}

// FakePodService build the minimal k8s objects required to discover one endpoint.
// If ServicePorts is empty a default of http-80 will be used.
func FakePodService(opts FakeServiceOpts) []runtime.Object {
	baseMeta := v12.ObjectMeta{
		Name:      opts.Name,
		Labels:    labels.Instance{"app": opts.Name},
		Namespace: opts.Namespace,
	}
	podMeta := baseMeta
	podMeta.Name = opts.Name + "-%s"
	for k, v := range opts.PodLabels {
		podMeta.Labels[k] = v
	}

	if len(opts.ServicePorts) == 0 {
		opts.ServicePorts = []v1.ServicePort{{
			Port:     80,
			Name:     "http",
			Protocol: v1.ProtocolTCP,
		}}
	}
	var endpointPorts []v1.EndpointPort
	for _, sp := range opts.ServicePorts {
		endpointPorts = append(endpointPorts, v1.EndpointPort{
			Name:        sp.Name,
			Port:        sp.Port,
			Protocol:    sp.Protocol,
			AppProtocol: sp.AppProtocol,
		})
	}

	ep := &v1.Endpoints{
		ObjectMeta: baseMeta,
		Subsets: []v1.EndpointSubset{{
			Addresses: []v1.EndpointAddress{},
			Ports:     endpointPorts,
		}},
	}
	objects := []runtime.Object{
		&v1.Service{
			ObjectMeta: baseMeta,
			Spec: v1.ServiceSpec{
				ClusterIP: "1.2.3.4", // just can't be 0.0.0.0/ClusterIPNone
				Selector:  baseMeta.Labels,
				Ports:     opts.ServicePorts,
			},
		},
		ep,
	}

	for _, ip := range opts.PodIPs {
		podMeta := podMeta
		podMeta.Name = fmt.Sprintf(podMeta.Name, rand.String(4))
		objects = append(objects,
			&v1.Pod{
				ObjectMeta: podMeta,
				Status: v1.PodStatus{
					PodIP: ip,
				},
			})
		ep.Subsets[0].Addresses = append(ep.Subsets[0].Addresses,
			v1.EndpointAddress{
				IP: ip,
				TargetRef: &v1.ObjectReference{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       podMeta.Name,
					Namespace:  podMeta.Namespace,
				},
			},
		)
	}

	return objects
}

