//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package distributor

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"istio.io/istio/galley/pkg/api/distrib"
	"istio.io/istio/galley/pkg/change"
	"istio.io/istio/galley/pkg/kube"
	"istio.io/istio/galley/pkg/kube/client"
	"istio.io/istio/galley/pkg/kube/convert"
	"istio.io/istio/galley/pkg/kube/types"
	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/pkg/log"
)

type Distributor struct {
	lock sync.Mutex
	k    kube.Kube

	processIndicator sync.WaitGroup

	rules     *client.Accessor
	ruleState map[string]*info

	current *distrib.MixerConfig
}

type state int

const (
	unknown state = iota
	synced
	deleting
	upserting
)

type info struct {
	state            state
	lastKnownVersion string
}

var _ runtime.Distributor = &Distributor{}

func New(k kube.Kube, resyncPeriod time.Duration) (*Distributor, error) {

	d := &Distributor{
		k: k,

		ruleState: make(map[string]*info),
	}

	rules, err := client.NewAccessor(
		k,
		resyncPeriod,
		types.Rule,
		func(c *change.Info) {
			d.processChange(c, d.ruleState)
		})

	if err != nil {
		return nil, err
	}

	d.rules = rules

	// 1 for source
	// 1 for distrib/rules
	d.processIndicator.Add(2)
	go d.process()

	return d, nil
}

func (d *Distributor) process() {
	for {
		d.processIndicator.Wait() // Wait until full sync
	}
}

func (d *Distributor) Initialize() error {
	return nil
}

func (d *Distributor) Start() {
	log.Info("Start")
	d.processIndicator.Done()
}

func (d *Distributor) Shutdown() {

}

func (d *Distributor) Dispatch(config *distrib.MixerConfig) error {
	b, _ := yaml.Marshal(config)
	log.Infof("Dispatch: %v", string(b))

	d.current = config

	iface, err := d.k.DynamicInterface(types.Rule.GroupVersion(), types.Rule.Singular, types.Rule.ListKind)
	if err != nil {
		return err
	}

	riface := iface.Resource(types.Rule.APIResource(), "istio-system")

	err = riface.DeleteCollection(&metav1.DeleteOptions{}, metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, r := range config.Rules {
		uns, err := toUnstructuredRule(r)
		if err != nil {
			log.Errorf("Error: %v", err)
			return err
		}

		if _, err = riface.Create(uns); err != nil {
			log.Errorf("Error: %v", err)
			return err
		}
	}

	return nil
}

func toUnstructuredRule(r *distrib.Rule) (*unstructured.Unstructured, error) {
	u, err := convert.ToUnstructured(types.Rule, r)
	if err != nil {
		return nil, err
	}

	name := uuid.New().String()
	u.SetName(name)
	u.SetNamespace("istio-system")

	return u, nil
}

func (d *Distributor) processChange(c *change.Info, states map[string]*info) {
	d.lock.Lock()
	defer d.lock.Unlock()

	switch c.Type {
	case change.Add, change.Update:
		if st, ok := states[c.Name]; !ok {
			// We don't know about this. Mark "unknown" for the time being until the processor can handle it.
			st = &info{
				state: unknown,
			}
			states[c.Name] = st
		}
	case change.Delete:
		if _, ok := states[c.Name]; ok {
			delete(states, c.Name)
		}

	case change.FullSync:

	default:
		log.Errorf("Unknown change info: %v", c)
	}

	d.processIndicator.Done()
}
