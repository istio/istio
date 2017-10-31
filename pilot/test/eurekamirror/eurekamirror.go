// Copyright 2017 Istio Authors
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

// Mirrors Kubernetes endpoint instances into an Eureka server.
//
// The Eureka mirror process watches endpoints in Kubernetes, converts
// endpoint instances to Eureka instances and maintains their registration
// with an Eureka server. The Eureka mirror processes the corresponding
// pod labels for the endpoint instances and converts them into Eureka
// metadata during the conversion.
//
// TODO: reduce dup between Eureka clients here and inside the Eureka adapter

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"

	"istio.io/pilot/model"
	"istio.io/pilot/platform/kube"
)

var (
	kubeconfig string
	eurekaURL  string
	namespace  string
)

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	flag.StringVar(&eurekaURL, "url", "",
		"Eureka server url")
	flag.StringVar(&namespace, "namespace", "",
		"Select a namespace for the controller loop. If not set, uses ${POD_NAMESPACE} environment variable")
}

type registerInstance struct {
	Instance instance `json:"instance"`
}

type instance struct { // nolint: aligncheck
	ID         string   `json:"instanceId,omitempty"`
	Hostname   string   `json:"hostName"`
	App        string   `json:"app"`
	IPAddress  string   `json:"ipAddr"`
	Port       port     `json:"port,omitempty"`
	SecurePort port     `json:"securePort,omitempty"`
	Metadata   metadata `json:"metadata,omitempty"`
}

type port struct {
	Port    int  `json:"$,string"`
	Enabled bool `json:"@enabled,string"`
}

type metadata map[string]string

const (
	heartbeatInterval = 30 * time.Second
	retryInterval     = 10 * time.Second
	appPath           = "%s/eureka/v2/apps/%s"
	instancePath      = "%s/eureka/v2/apps/%s/%s"
)

// agent is a simple state machine that maintains an instance's registration with Eureka.
type agent struct {
	stop     <-chan struct{}
	client   http.Client
	url      string
	instance *instance
}

func maintainRegistration(url string, client http.Client, inst *instance, stop <-chan struct{}) {
	log.Println("Starting registration agent for", inst.ID, "with hostname", inst.Hostname)
	a := &agent{
		stop:     stop,
		client:   client,
		url:      url,
		instance: inst,
	}
	go a.unregistered()
	<-stop
}

func (a *agent) unregistered() {
	var retry, heartbeatDelay <-chan time.Time
	if err := a.register(); err != nil {
		log.Println(err)
		retry = time.After(retryInterval)
	} else {
		heartbeatDelay = time.After(heartbeatInterval)
	}

	select {
	case <-retry:
		go a.unregistered() // attempt to re-register
	case <-heartbeatDelay:
		go a.registered() // start heartbeating
	case <-a.stop:
		log.Println("Stopping registration agent", a.instance.ID, "with hostname", a.instance.Hostname)
		return
	}
}

func (a *agent) registered() {
	var retry, heartbeatDelay <-chan time.Time
	if err := a.heartbeat(); err != nil {
		log.Println(err)
		retry = time.After(retryInterval)
	} else {
		heartbeatDelay = time.After(heartbeatInterval)
	}

	select { // TODO: unregistered vs heartbeat failure
	case <-retry:
		go a.unregistered()
	case <-heartbeatDelay:
		go a.registered()
	case <-a.stop:
		// attempt to unregister before terminating
		if err := a.unregister(); err != nil {
			log.Println(err)
		}
		log.Println("Stopping registration agent", a.instance.ID, "with hostname", a.instance.Hostname)
		return
	}
}

func (a *agent) register() error {
	payload := registerInstance{Instance: *a.instance}
	data, err := json.Marshal(&payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, a.buildRegisterPath(), bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() // nolint: errcheck

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status code %s", resp.Status)
	}
	return nil
}

func (a *agent) heartbeat() error {
	req, err := http.NewRequest(http.MethodPut, a.buildInstancePath(), nil)
	if err != nil {
		return err
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() // nolint: errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code %s", resp.Status)
	}
	return nil
}

func (a *agent) unregister() error {
	req, err := http.NewRequest(http.MethodDelete, a.buildInstancePath(), nil)
	if err != nil {
		return err
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to unregister %s, got %s", a.instance.ID, resp.Status)
	}
	return nil
}

func (a *agent) buildRegisterPath() string {
	return fmt.Sprintf(appPath, a.url, a.instance.App)
}

func (a *agent) buildInstancePath() string {
	return fmt.Sprintf(instancePath, a.url, a.instance.App, a.instance.ID)
}

type eventKind int

const (
	addEvent eventKind = iota
	deleteEvent
)

type event struct {
	kind eventKind
	obj  interface{}
}

// mirrors k8s instances to an Eureka server
// TODO: this may need flow control if enough events are being generated
type mirror struct {
	url string
	// mapping of endpoint name to instance id
	agents map[string]map[string]chan struct{}
	// mapping of ip to pod name
	podCache map[string]string
	podStore cache.Store
}

func maintainMirror(url string, podStore cache.Store, events <-chan event) {
	m := &mirror{
		url:      url,
		agents:   make(map[string]map[string]chan struct{}),
		podStore: podStore,
		podCache: make(map[string]string),
	}
	m.sync(events)
}

func (m *mirror) sync(events <-chan event) {
	for ev := range events {
		switch ev.obj.(type) {
		case *v1.Endpoints:
			endpoint := ev.obj.(*v1.Endpoints)
			switch ev.kind {
			case addEvent:
				instances := m.convertEndpoints(endpoint)

				newIDs := make(map[string]bool)
				for _, instance := range instances {
					newIDs[instance.ID] = true
				}

				agents, exists := m.agents[endpoint.Name]
				if !exists {
					m.agents[endpoint.Name] = make(map[string]chan struct{})
					agents = m.agents[endpoint.Name]
				}

				// remove instances that are gone
				toRemove := make([]string, 0)
				for id := range agents {
					if !newIDs[id] {
						toRemove = append(toRemove, id)
					}
				}
				for _, id := range toRemove {
					m.stopAgent(endpoint.Name, id)
				}

				// add instances that are new
				for _, instance := range instances {
					if _, exists := agents[instance.ID]; !exists {
						m.startAgent(endpoint.Name, instance)
					}
				}
			case deleteEvent:
				for id := range m.agents[endpoint.Name] {
					m.stopAgent(endpoint.Name, id)
				}
				delete(m.agents, endpoint.Name)
			}
		case *v1.Pod:
			pod := ev.obj.(*v1.Pod)
			switch ev.kind {
			case addEvent:
				if pod.Status.PodIP != "" {
					m.podCache[pod.Status.PodIP] = podKey(pod)
				}
			case deleteEvent:
				delete(m.podCache, podKey(pod))
			}
		}
	}

	// cleanup registration agents
	for name := range m.agents {
		for id := range m.agents[name] {
			m.stopAgent(name, id)
		}
	}
}

func (m *mirror) convertEndpoints(ep *v1.Endpoints) []*instance {
	instances := make([]*instance, 0)
	for _, ss := range ep.Subsets {
		for _, addr := range ss.Addresses {
			for _, ssPort := range ss.Ports {
				md := make(metadata)

				// add labels
				pod, exists := m.getPodByIP(addr.IP)
				if exists {
					for k, v := range pod.Labels {
						md[k] = v
					}
				}

				// add protocol labels
				protocol := kube.ConvertProtocol(ssPort.Name, ssPort.Protocol)
				switch protocol {
				case model.ProtocolUDP:
					md["istio.protocol"] = "udp"
				case model.ProtocolTCP:
					md["istio.protocol"] = "tcp"
				case model.ProtocolHTTP:
					md["istio.protocol"] = "http"
				case model.ProtocolHTTP2:
					md["istio.protocol"] = "http2"
				case model.ProtocolHTTPS:
					md["istio.protocol"] = "https"
				case model.ProtocolGRPC:
					md["istio.protocol"] = "grpc"
				}

				instances = append(instances, &instance{
					ID:        fmt.Sprintf("%s-%s-%d", ep.ObjectMeta.Name, addr.IP, ssPort.Port),
					App:       ep.ObjectMeta.Name,
					Hostname:  fmt.Sprintf("%s.%s.svc.cluster.local", ep.Name, ep.Namespace),
					IPAddress: addr.IP,
					Port: port{
						Port:    int(ssPort.Port),
						Enabled: true,
					},
					Metadata: md,
				})
			}
		}
	}
	return instances
}

func (m *mirror) getPodByIP(addr string) (*v1.Pod, bool) {
	name, exists := m.podCache[addr]
	if !exists {
		return nil, false
	}
	obj, exists, err := m.podStore.GetByKey(name)
	if err != nil {
		log.Println(err)
	}
	if !exists {
		return nil, false
	}
	return obj.(*v1.Pod), true
}

func (m *mirror) startAgent(name string, inst *instance) {
	stop := make(chan struct{})
	m.agents[name][inst.ID] = stop
	go maintainRegistration(m.url, http.Client{Timeout: 15 * time.Second}, inst, stop)
}

func (m *mirror) stopAgent(name, id string) {
	close(m.agents[name][id])
	delete(m.agents[name], id)
}

func podKey(pod *v1.Pod) string {
	return pod.Namespace + "/" + pod.Name
}

func main() {
	flag.Parse()

	_, client, err := kube.CreateInterface(kubeconfig)
	if err != nil {
		log.Println(err)
		return
	}

	events := make(chan event)
	handler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			events <- event{addEvent, obj}
		},
		UpdateFunc: func(old, cur interface{}) {
			events <- event{deleteEvent, old}
			events <- event{addEvent, cur}
		},
		DeleteFunc: func(obj interface{}) {
			events <- event{deleteEvent, obj}
		},
	}

	endpointInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(opts meta_v1.ListOptions) (runtime.Object, error) {
				return client.CoreV1().Endpoints(namespace).List(opts)
			},
			WatchFunc: func(opts meta_v1.ListOptions) (watch.Interface, error) {
				return client.CoreV1().Endpoints(namespace).Watch(opts)
			},
		},
		&v1.Endpoints{}, 1*time.Second, cache.Indexers{},
	)
	endpointInformer.AddEventHandler(handler)

	podInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(opts meta_v1.ListOptions) (runtime.Object, error) {
				return client.CoreV1().Pods(namespace).List(opts)
			},
			WatchFunc: func(opts meta_v1.ListOptions) (watch.Interface, error) {
				return client.CoreV1().Pods(namespace).Watch(opts)
			},
		},
		&v1.Pod{}, 1*time.Second, cache.Indexers{},
	)
	podInformer.AddEventHandler(handler)

	stop := make(chan struct{})

	go endpointInformer.Run(stop)
	go podInformer.Run(stop)
	go maintainMirror(eurekaURL, podInformer.GetStore(), events)

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	sig := <-c
	log.Printf("captured sig %v, exiting\n", sig)
	close(stop)
	close(events)
	os.Exit(1)
}
