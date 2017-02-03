// Copyright 2016 Google Ina.
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

	"gopkg.in/yaml.v2"
	"istio.io/mixer/adapter/ipListChecker/config"
	"istio.io/mixer/pkg/adapter"
)

type (
	builder struct{ adapter.DefaultBuilder }

	listChecker struct {
		log             adapter.Logger
		providerURL     string
		atomicList      atomic.Value
		refreshInterval time.Duration
		ttl             time.Duration
		closing         chan bool
		client          http.Client
	}

	listState struct {
		payload    []*net.IPNet
		payloadSha [sha1.Size]byte
		fetchError error
	}
)

var (
	name = "ipListChecker"
	desc = "Checks whether an IP address is present in an IP address list."
	conf = &config.Params{
		ProviderUrl:     "http://localhost",
		RefreshInterval: 60,
		Ttl:             120,
	}
)

// Register records the builders exposed by this adapter.
func Register(r adapter.Registrar) {
	r.RegisterListsBuilder(newBuilder())
}

func newBuilder() adapter.ListsBuilder {
	return builder{adapter.NewDefaultBuilder(name, desc, conf)}
}

func (builder) NewListsAspect(env adapter.Env, c adapter.AspectConfig) (adapter.ListsAspect, error) {
	return newListChecker(env, c.(*config.Params))
}

func (builder) ValidateConfig(cfg adapter.AspectConfig) (ce *adapter.ConfigErrors) {
	c := cfg.(*config.Params)

	u, err := url.Parse(c.ProviderUrl)
	if err != nil {
		ce = ce.Append("ProviderUrl", err)
	} else {
		if u.Scheme == "" || u.Host == "" {
			ce = ce.Appendf("ProviderUrl", "URL scheme and host cannot be empty")
		}
	}

	return
}

func newListChecker(env adapter.Env, c *config.Params) (*listChecker, error) {
	l := &listChecker{
		log:             env.Logger(),
		providerURL:     c.ProviderUrl,
		closing:         make(chan bool),
		refreshInterval: time.Second * time.Duration(c.RefreshInterval),
		ttl:             time.Second * time.Duration(c.Ttl),
	}
	l.client = http.Client{Timeout: l.ttl}
	l.setList(listState{})

	// load up the list synchronously so we're ready to accept traffic immediately
	l.refreshList()

	// crank up the async list refresher
	go l.listRefresher()

	return l, nil
}

func (l *listChecker) Close() error {
	close(l.closing)
	return nil
}

func (l *listChecker) CheckList(symbol string) (bool, error) {
	ipa := net.ParseIP(symbol)
	if ipa == nil {
		// invalid symbol format
		return false, errors.New(symbol + " is not a valid IP address")
	}

	// get an atomic snapshot of the current list
	list := l.getList()
	if len(list.payload) == 0 {
		return false, list.fetchError
	}

	for _, ipnet := range list.payload {
		if ipnet.Contains(ipa) {
			return true, nil
		}
	}

	// not found in the list
	return false, nil
}

// Typed accessors for the atomic list
func (l *listChecker) getList() listState {
	return l.atomicList.Load().(listState)
}

// Typed accessor for the atomic list
func (l *listChecker) setList(list listState) {
	l.atomicList.Store(list)
}

// Updates the list by polling from the provider on a fixed interval
func (l *listChecker) listRefresher() {
	refreshTicker := time.NewTicker(l.refreshInterval)
	purgeTimer := time.NewTimer(l.ttl)

	defer refreshTicker.Stop()
	defer purgeTimer.Stop()

	for {
		select {
		case <-refreshTicker.C:
			// fetch a new list and reset the TTL timer
			l.refreshList()
			purgeTimer.Reset(l.ttl)

		case <-purgeTimer.C:
			// times up, nuke the list and start returning errors
			list := l.getList()
			list.payload = nil
			list.payloadSha = [20]byte{}
			l.setList(list)

		case <-l.closing:
			return
		}
	}
}

// represents the format of the data in a list
type listPayload struct {
	WhiteList []string `yaml:"whitelist" required:"true"`
}

func (l *listChecker) refreshList() {
	l.log.Infof("Fetching list from %s", l.providerURL)

	list := l.getList()

	resp, err := l.client.Get(l.providerURL)
	if err != nil {
		list.fetchError = err
		l.setList(list)
		l.log.Warningf("Could not connect to %s: %v", l.providerURL, err)
		return
	}

	// TODO: could lead to OOM since this is unbounded
	var buf []byte
	buf, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		list.fetchError = err
		l.setList(list)
		l.log.Warningf("Could not read from %s: %v", l.providerURL, err)
		return
	}

	// determine whether the list has changed since the last fetch
	newsha := sha1.Sum(buf)
	if newsha == list.payloadSha {
		// the list hasn't changed since last time, just bail
		l.log.Infof("Fetched list is unchanged")
		return
	}

	// now parse
	lp := listPayload{}
	err = yaml.Unmarshal(buf, &lp)
	if err != nil {
		list.fetchError = err
		l.setList(list)
		l.log.Warningf("Could not unmarshal data from %s: %v", l.providerURL, err)
		return
	}

	// copy to the internal format
	entries := make([]*net.IPNet, 0, len(lp.WhiteList))
	for _, ip := range lp.WhiteList {
		if !strings.Contains(ip, "/") {
			ip += "/32"
		}

		_, ipnet, err := net.ParseCIDR(ip)
		if err != nil {
			l.log.Warningf("Skipping unparsable entry %s: %v", ip, err)
			continue
		}
		entries = append(entries, ipnet)
	}

	// Now create a new map and install it
	l.log.Infof("Installing updated list with %d entries", len(entries))
	list.payload = entries
	list.payloadSha = newsha
	list.fetchError = nil
	l.setList(list)
}
