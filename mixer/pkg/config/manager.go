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
	"io/ioutil"
	"sync"
	"time"

	"github.com/golang/glog"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config/descriptor"
	pb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
)

// Resolver resolves configuration to a list of combined configs.
type Resolver interface {
	// resolve resolves configuration to a list of combined configs.
	Resolve(bag attribute.Bag, kindSet KindSet) ([]*pb.Combined, error)
}

// ChangeListener listens for config change notifications.
type ChangeListener interface {
	ConfigChange(cfg Resolver, df descriptor.Finder)
}

// Manager represents the config Manager.
// It is responsible for fetching and receiving configuration changes.
// It applies validated changes to the registered config change listeners.
// api.Handler listens for config changes.
type Manager struct {
	eval             expr.Evaluator
	aspectFinder     AspectValidatorFinder
	builderFinder    BuilderValidatorFinder
	descriptorFinder descriptor.Finder
	findAspects      AdapterToAspectMapper
	loopDelay        time.Duration
	globalConfig     string
	serviceConfig    string
	ticker           *time.Ticker

	cl    []ChangeListener
	scSHA [sha1.Size]byte
	gcSHA [sha1.Size]byte

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
	findAspects AdapterToAspectMapper, globalConfig string, serviceConfig string, loopDelay time.Duration) *Manager {
	m := &Manager{
		eval:          eval,
		aspectFinder:  aspectFinder,
		builderFinder: builderFinder,
		findAspects:   findAspects,
		loopDelay:     loopDelay,
		globalConfig:  globalConfig,
		serviceConfig: serviceConfig,
	}
	return m
}

// Register makes the ConfigManager aware of a ConfigChangeListener.
func (c *Manager) Register(cc ChangeListener) {
	c.cl = append(c.cl, cc)
}

func read(fname string) ([sha1.Size]byte, string, error) {
	var data []byte
	var err error
	if data, err = ioutil.ReadFile(fname); err != nil {
		return [sha1.Size]byte{}, "", err
	}
	return sha1.Sum(data), string(data[:]), nil
}

// fetch config and return runtime if a new one is available.
func (c *Manager) fetch() (*runtime, descriptor.Finder, error) {
	var vd *Validated
	var cerr *adapter.ConfigErrors

	gcSHA, gc, err2 := read(c.globalConfig)
	if err2 != nil {
		return nil, nil, err2
	}

	scSHA, sc, err1 := read(c.serviceConfig)
	if err1 != nil {
		return nil, nil, err1
	}

	if gcSHA == c.gcSHA && scSHA == c.scSHA {
		return nil, nil, nil
	}

	v := newValidator(c.aspectFinder, c.builderFinder, c.findAspects, true, c.eval)
	if vd, cerr = v.validate(sc, gc); cerr != nil {
		return nil, nil, cerr
	}

	c.descriptorFinder = descriptor.NewFinder(v.validated.globalConfig)

	c.gcSHA = gcSHA
	c.scSHA = scSHA
	return newRuntime(vd, c.eval), c.descriptorFinder, nil
}

// fetchAndNotify fetches a new config and notifies listeners if something has changed
func (c *Manager) fetchAndNotify() error {
	rt, df, err := c.fetch()
	if err != nil {
		c.Lock()
		c.lastError = err
		c.Unlock()
		return err
	}
	if rt == nil {
		return nil
	}

	glog.Infof("Installing new config from %s sha=%x ", c.serviceConfig, c.scSHA)
	for _, cl := range c.cl {
		cl.ConfigChange(rt, df)
	}
	return nil
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
		err := c.fetchAndNotify()
		if err != nil {
			glog.Warning(err)
		}
	}
}

// Start watching for configuration changes and handle updates.
func (c *Manager) Start() {
	err := c.fetchAndNotify()
	// We make an attempt to synchronously fetch and notify configuration
	// If it is not successful, we will continue to watch for changes.
	c.ticker = time.NewTicker(c.loopDelay)
	go c.loop()
	if err != nil {
		glog.Warning("Unable to process config: ", err)
	}
}
