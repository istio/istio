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
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/duration"
	"gopkg.in/yaml.v2"

	"istio.io/mixer/adapter/ipListChecker/config"
	"istio.io/mixer/pkg/adapter"
)

type (
	builder struct{ adapter.DefaultBuilder }

	listChecker struct {
		log           adapter.Logger
		providerURL   string
		atomicList    atomic.Value
		client        http.Client
		closing       chan bool
		refreshTicker *time.Ticker
		purgeTimer    *time.Timer
		ttl           time.Duration
	}

	listState struct {
		entries    []*net.IPNet
		entriesSHA [sha1.Size]byte
		fetchError error
	}
)

var (
	name = "ipListChecker"
	desc = "Checks whether an IP address is present in an IP address list."
	conf = &config.Params{
		ProviderUrl:     "http://localhost",
		RefreshInterval: &duration.Duration{Seconds: 60},
		Ttl:             &duration.Duration{Seconds: 300},
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

	refresh, err := ptypes.Duration(c.RefreshInterval)
	if err != nil {
		ce = ce.Append("RefreshInterval", err)
	} else if refresh <= 0 {
		ce = ce.Appendf("RefreshInterval", "refresh interval must be > 0, it is %v", refresh)
	}

	ttl, err := ptypes.Duration(c.Ttl)
	if err != nil {
		ce = ce.Append("Ttl", err)
	} else if ttl < refresh {
		ce = ce.Appendf("Ttl", "Ttl must be > RefreshInterval, Ttl is %v and RefreshInterval is %v", ttl, refresh)
	}

	return
}

func newListChecker(env adapter.Env, c *config.Params) (*listChecker, error) {
	refresh, _ := ptypes.Duration(c.RefreshInterval)
	ttl, _ := ptypes.Duration(c.Ttl)

	return newListCheckerWithTimers(env, c, time.NewTicker(refresh), time.NewTimer(ttl), ttl)
}

func newListCheckerWithTimers(env adapter.Env, c *config.Params,
	refreshTicker *time.Ticker, purgeTimer *time.Timer, ttl time.Duration) (*listChecker, error) {
	l := &listChecker{
		log:           env.Logger(),
		providerURL:   c.ProviderUrl,
		closing:       make(chan bool),
		refreshTicker: refreshTicker,
		purgeTimer:    purgeTimer,
		ttl:           ttl,
	}
	l.setListState(listState{})

	// Load up the list synchronously so we're ready to accept traffic immediately.
	// Failures here don't matter, they will be logged appropriately by fetchList.
	l.fetchList()

	// go routine to periodically refresh the list
	go l.listRefresher()

	return l, nil
}

func (l *listChecker) Close() error {
	close(l.closing)
	l.refreshTicker.Stop()
	l.purgeTimer.Stop()
	return nil
}

func (l *listChecker) CheckList(symbol string) (bool, error) {
	ipa := net.ParseIP(symbol)
	if ipa == nil {
		// invalid symbol format
		return false, errors.New(symbol + " is not a valid IP address")
	}

	// get an atomic snapshot of the current list
	ls := l.getListState()
	if len(ls.entries) == 0 {
		return false, ls.fetchError
	}

	for _, ipnet := range ls.entries {
		if ipnet.Contains(ipa) {
			return true, nil
		}
	}

	// not found in the list
	return false, nil
}

// Typed accessors for the atomic list
func (l *listChecker) getListState() listState {
	return l.atomicList.Load().(listState)
}

// Typed accessor for the atomic list
func (l *listChecker) setListState(ls listState) {
	l.atomicList.Store(ls)
}

// Updates the list by polling from the provider on a fixed interval
func (l *listChecker) listRefresher() {
	for {
		select {
		case <-l.refreshTicker.C:
			l.fetchList()

		case <-l.purgeTimer.C:
			l.purgeList()

		case <-l.closing:
			return
		}
	}
}

// represents the format of the data in a list
type listPayload struct {
	WhiteList []string `yaml:"whitelist" required:"true"`
}

func (l *listChecker) fetchList() {
	l.log.Infof("Fetching list from %s", l.providerURL)

	resp, err := l.client.Get(l.providerURL)
	if err != nil {
		ls := l.getListState()
		ls.fetchError = l.log.Errorf("Could not connect to %s: %v", l.providerURL, err)
		l.setListState(ls)
		return
	}

	l.fetchListWithBody(resp.Body)
}

func (l *listChecker) fetchListWithBody(body io.Reader) {
	ls := l.getListState()

	// TODO: could lead to OOM since this is unbounded
	buf, err := ioutil.ReadAll(body)
	if err != nil {
		ls.fetchError = l.log.Errorf("Could not read from %s: %v", l.providerURL, err)
		l.setListState(ls)
		return
	}

	// determine whether the list has changed since the last fetch
	newSHA := sha1.Sum(buf)
	if newSHA == ls.entriesSHA {
		// the list hasn't changed since last time, just bail
		l.log.Infof("Fetched list is unchanged")
		return
	}

	// now parse
	lp := listPayload{}
	err = yaml.Unmarshal(buf, &lp)
	if err != nil {
		ls.fetchError = l.log.Errorf("Could not unmarshal data from %s: %v", l.providerURL, err)
		l.setListState(ls)
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

	// Now install the new list
	l.log.Infof("Installing updated list with %d entries", len(entries))
	ls.entries = entries
	ls.entriesSHA = newSHA
	ls.fetchError = nil
	l.setListState(ls)

	// prevent the outstanding purge from happening
	l.purgeTimer.Stop()

	// clean up the channel in case a message is already pending
	select {
	case <-l.purgeTimer.C:
	default:
	}

	// setup the purge timer to clean up the list if it doesn't get refreshed soon enough
	l.purgeTimer.Reset(l.ttl)
}

func (l *listChecker) purgeList() {
	l.log.Warningf("Purging list due to inability to refresh in time")

	ls := l.getListState()
	ls.entries = nil
	ls.entriesSHA = [20]byte{}
	l.setListState(ls)
}
