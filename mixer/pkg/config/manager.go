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

package config

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/golang/glog"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/config/descriptor"
	pb "istio.io/istio/mixer/pkg/config/proto"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/expr"
	"istio.io/istio/mixer/pkg/template"
)

// Resolver resolves configuration to a list of combined configs.
type Resolver interface {
	// Resolve resolves configuration to a list of combined configs.
	Resolve(bag attribute.Bag, kindSet KindSet, strict bool) ([]*pb.Combined, error)
	// ResolveUnconditional resolves configuration for unconditioned rules.
	// Unconditioned rules are those rules with the empty selector ("").
	ResolveUnconditional(bag attribute.Bag, kindSet KindSet, strict bool) ([]*pb.Combined, error)
}

// ChangeListener listens for config change notifications.
type ChangeListener interface {
	ConfigChange(cfg Resolver, df descriptor.Finder, handlers map[string]*HandlerInfo)
}

// Manager represents the config Manager.
// It is responsible for fetching and receiving configuration changes.
// It applies validated changes to the registered config change listeners.
// api.Handler listens for config changes.
type Manager struct {
	eval          expr.Evaluator
	aspectFinder  AspectValidatorFinder
	builderFinder BuilderValidatorFinder
	findAspects   AdapterToAspectMapper
	loopDelay     time.Duration
	store         store.KeyValueStore
	validate      validateFunc

	// attribute around which scopes and subjects are organized.
	identityAttribute       string
	identityAttributeDomain string

	ticker         *time.Ticker
	lastFetchIndex int

	cl []ChangeListener

	lastValidated *Validated
	sync.RWMutex
	lastError error
}

// NewManager returns a config.Manager.
// Eval validates and evaluates selectors.
// It is also used downstream for attribute mapping.
// AspectFinder finds aspect validator given aspect 'Kind'.
// BuilderFinder finds builder validator given builder 'Impl'.
// LoopDelay determines how often configuration is updated.
// The following fields will be eventually replaced by a
// repository location. At present we use GlobalConfig and ServiceConfig
// as command line input parameters.
// GlobalConfig specifies the location of Global Config.
// ServiceConfig specifies the location of Service config.
func NewManager(eval expr.Evaluator, aspectFinder AspectValidatorFinder, builderFinder BuilderValidatorFinder,
	getBuilderInfoFns []adapter.InfoFn, findAspects AdapterToAspectMapper, repository template.Repository,
	store store.KeyValueStore, loopDelay time.Duration, identityAttribute string,
	identityAttributeDomain string) *Manager {
	m := &Manager{
		eval:                    eval,
		aspectFinder:            aspectFinder,
		builderFinder:           builderFinder,
		findAspects:             findAspects,
		loopDelay:               loopDelay,
		store:                   store,
		identityAttribute:       identityAttribute,
		identityAttributeDomain: identityAttributeDomain,
		validate: func(cfg map[string]string) (*Validated, descriptor.Finder, *adapter.ConfigErrors) {
			r := newRegistry2(getBuilderInfoFns, repository.SupportsTemplate)
			v := newValidator(aspectFinder, builderFinder, r.FindAdapterInfo, SetupHandlers, repository, findAspects, true, eval)
			rt, ce := v.validate(cfg)
			return rt, v.descriptorFinder, ce
		},
	}

	return m
}

// StoreChange is called by "store" when new changes are available
//func (c *Manager) StoreChange(index int) {
//	// fetchAndNotify already logs errors
//	c.fetchAndNotify()
//}

// Register makes the ConfigManager aware of a ConfigChangeListener.
func (c *Manager) Register(cc ChangeListener) {
	c.cl = append(c.cl, cc)
}

func readdb(store store.KeyValueStore, prefix string) (map[string]string, map[string][sha1.Size]byte, int, error) { // nolint: unparam
	keys, index, err := store.List(prefix, true)
	if err != nil {
		return nil, nil, index, err
	}

	// read
	shas := map[string][sha1.Size]byte{}
	data := map[string]string{}

	var found bool
	var val string

	for _, k := range keys {
		val, index, found = store.Get(k)
		if !found {
			continue
		}
		data[k] = val
		shas[k] = sha1.Sum([]byte(val))
	}

	return data, shas, index, nil
}

//  /scopes/global/subjects/global/rules
//  /scopes/global/adapters
//  /scopes/global/descriptors

// fetch config and return runtime if a new one is available.
func (c *Manager) fetch() (*runtime, descriptor.Finder, map[string]*HandlerInfo, error) {

	data, shas, index, err := readdb(c.store, "/")
	if glog.V(9) {
		glog.Infof("Fetched config payload:\n%v", data)
	}
	if err != nil {
		return nil, nil, nil, errors.New("Unable to read database: " + err.Error())
	}
	// check if sha has changed.
	if c.lastValidated != nil && reflect.DeepEqual(shas, c.lastValidated.shas) {
		// nothing actually changed.
		return nil, nil, nil, nil
	}

	// TODO Close the handlers that were previously part of old runtime object.
	// Only close the handlers whose config has changed or their inferred types have changed.
	// Information about all of that in computed inside config/handler.go
	var vd *Validated
	var finder descriptor.Finder
	var cerr *adapter.ConfigErrors

	vd, finder, cerr = c.validate(data)
	if cerr != nil {
		glog.Warningf("Validation failed: %v", cerr)
		fmt.Println(cerr)
		return nil, nil, nil, cerr
	}
	if glog.V(4) {
		glog.Infof("Fetched and validated config payload:\n%v", data)
	}
	c.lastFetchIndex = index
	vd.shas = shas
	c.lastValidated = vd
	return newRuntime(vd, c.eval, c.identityAttribute, c.identityAttributeDomain), finder, vd.handlers, nil
}

// fetchAndNotify fetches a new config and notifies listeners if something has changed
func (c *Manager) fetchAndNotify() {
	rt, df, handlers, err := c.fetch()
	if err != nil {
		c.Lock()
		c.lastError = err
		c.Unlock()
		glog.Warningf("Error loading new config %v", err)
	}
	if rt == nil {
		return
	}

	for _, cl := range c.cl {
		cl.ConfigChange(rt, df, handlers)
	}
}

// LastError returns last error encountered by the manager while processing config.
func (c *Manager) LastError() (err error) {
	c.RLock()
	err = c.lastError
	c.RUnlock()
	return err
}

// Close stops the config manager go routine.
func (c *Manager) Close() {
	if c.ticker != nil {
		c.ticker.Stop()
	}
}

func (c *Manager) loop() {
	for range c.ticker.C {
		c.fetchAndNotify()
	}
}

// Start watching for configuration changes and handle updates.
func (c *Manager) Start() {
	c.fetchAndNotify()
	// FIXME add change notifier registration once we have
	// an adapter that supports it cn.RegisterStoreChangeListener(c)

	// if store does not support notification use the loop
	// If it is not successful, we will continue to watch for changes.
	c.ticker = time.NewTicker(c.loopDelay)
	go c.loop()
}
