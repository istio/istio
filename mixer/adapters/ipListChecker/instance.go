// Copyright 2016 Google Ininst.
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

package ipListChecker

import (
	"crypto/sha1"
	"errors"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/golang/glog"
	"gopkg.in/yaml.v2"
	"istio.io/mixer/adapters"
)

// InstanceConfig is used to configure instances.
type InstanceConfig struct {
	adapters.InstanceConfig

	// ProviderURL is the URL where to find the list to check against
	ProviderURL string

	// RefreshInterval determines how often the provider is polled for
	// an updated list.
	RefreshInterval time.Duration

	// TimeToLive indicates how long to keep a list before discarding it.
	// Typically, the TTL value should be set to noticeably longer (> 2x) than the
	// refresh interval to ensure continued operation in the face of transient
	// server outages.
	TimeToLive time.Duration
}

type instance struct {
	backend         *url.URL
	atomicList      atomic.Value
	fetchedSha      [sha1.Size]byte
	refreshInterval time.Duration
	ttl             time.Duration
	closing         chan bool
}

// newInstance returns a new instance of the adapter
func newInstance(config *InstanceConfig) (adapters.ListChecker, error) {
	var u *url.URL
	var err error
	if u, err = url.Parse(config.ProviderURL); err != nil {
		// bogus URL format
		return nil, err
	}

	inst := instance{
		backend:         u,
		closing:         make(chan bool),
		refreshInterval: config.RefreshInterval,
		ttl:             config.TimeToLive,
	}

	// install an empty list
	inst.setList([]*net.IPNet{})

	// load up the list synchronously so we're ready to accept traffic immediately
	inst.refreshList()

	// crank up the async list refresher
	go inst.listRefresher()

	return &inst, nil
}

func (inst *instance) Delete() {
	close(inst.closing)
}

func (inst *instance) UpdateConfig(config adapters.InstanceConfig) error {
	return errors.New("not implemented")
}

func (inst *instance) CheckList(symbol string) (bool, error) {
	ipa := net.ParseIP(symbol)
	if ipa == nil {
		// invalid symbol format
		return false, errors.New(symbol + " is not a valid IP address")
	}

	// get an atomic snapshot of the current list
	l := inst.getList()
	if l == nil {
		// TODO: would be nice to return the last I/O error received from the provider...
		return false, errors.New("list provider is unreachable")
	}

	for _, ipnet := range l {
		if ipnet.Contains(ipa) {
			return true, nil
		}
	}

	// not found in the list
	return false, nil
}

// Typed accessors for the atomic list
func (inst *instance) getList() []*net.IPNet {
	return inst.atomicList.Load().([]*net.IPNet)
}

// Typed accessor for the atomic list
func (inst *instance) setList(l []*net.IPNet) {
	inst.atomicList.Store(l)
}

// Updates the list by polling from the provider on a fixed interval
func (inst *instance) listRefresher() {
	refreshTicker := time.NewTicker(inst.refreshInterval)
	purgeTimer := time.NewTimer(inst.ttl)

	defer refreshTicker.Stop()
	defer purgeTimer.Stop()

	for {
		select {
		case <-refreshTicker.C:
			// fetch a new list and reset the TTL timer
			inst.refreshList()
			purgeTimer.Reset(inst.ttl)

		case <-purgeTimer.C:
			// times up, nuke the list and start returning errors
			inst.setList(nil)

		case <-inst.closing:
			return
		}
	}
}

// represents the format of the data in a list
type listPayload struct {
	WhiteList []string `yaml:"whitelist" required:"true"`
}

func (inst *instance) refreshList() {
	glog.Infoln("Fetching list from ", inst.backend.String())

	client := http.Client{Timeout: inst.refreshInterval}
	resp, err := client.Get(inst.backend.String())
	if err != nil {
		glog.Warning("Could not connect to ", inst.backend.String(), " ", err)
		return
	}

	// TODO: could lead to OOM since this is unbounded
	var buf []byte
	buf, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		glog.Warning("Could not read from ", inst.backend.String(), " ", err)
		return
	}

	// determine whether the list has changed since the last fetch
	// Note that inst.fetchedSha is only read and written by this function
	// in a single thread
	newsha := sha1.Sum(buf)
	if newsha == inst.fetchedSha {
		// the list hasn't changed since last time, just bail
		glog.Infoln("Fetched list is unchanged")
		return
	}

	// now parse
	lp := listPayload{}
	err = yaml.Unmarshal(buf, &lp)
	if err != nil {
		glog.Warning("Could not unmarshal ", inst.backend.String(), " ", err)
		return
	}

	// copy to the internal format
	l := make([]*net.IPNet, 0, len(lp.WhiteList))
	for _, ip := range lp.WhiteList {
		if !strings.Contains(ip, "/") {
			ip += "/32"
		}

		_, ipnet, err := net.ParseCIDR(ip)
		if err != nil {
			glog.Warningf("Unable to parse %s -- %v", ip, err)
			continue
		}
		l = append(l, ipnet)
	}

	// Now create a new map and install it
	glog.Infoln("Installing updated list")
	inst.setList(l)
	inst.fetchedSha = newsha
}
