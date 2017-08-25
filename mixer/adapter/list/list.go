// Copyright 2017 Google Ina.
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

package list // import "istio.io/mixer/adapter/list"

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/mixer/adapter/list/config"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/template/listentry"
)

type (
	builder struct{}

	handler struct {
		log           adapter.Logger
		closing       chan bool
		refreshTicker *time.Ticker
		purgeTimer    *time.Timer
		config        config.Params

		lock           sync.Mutex
		list           list
		lastFetchError error

		latestSHA [sha1.Size]byte

		// indirection to enable fault injection
		readAll func(io.Reader) ([]byte, error)
	}

	// a specific list we use to check against
	list interface {
		checkList(symbol string) (bool, error)
		numEntries() int
	}
)

// ensure our types implement the requisite interfaces
var _ listentry.HandlerBuilder = &builder{}
var _ listentry.Handler = &handler{}

func (*builder) Build(cfg adapter.Config, env adapter.Env) (adapter.Handler, error) {
	c := cfg.(*config.Params)

	h := &handler{
		log:     env.Logger(),
		closing: make(chan bool),
		config:  *c,
		readAll: ioutil.ReadAll,
	}

	if c.ProviderUrl != "" {
		h.refreshTicker = time.NewTicker(c.RefreshInterval)
		h.purgeTimer = time.NewTimer(c.Ttl)
	}

	// Load up the list synchronously so we're ready to accept traffic immediately.
	h.fetchList()

	if c.ProviderUrl != "" {
		// goroutine to periodically refresh the list
		env.ScheduleDaemon(h.listRefresher)
	}

	return h, nil
}

func (*builder) ConfigureListEntryHandler(map[string]*listentry.Type) error {
	return nil
}

///////////////// Runtime Methods ///////////////

func (h *handler) HandleListEntry(_ context.Context, entry *listentry.Instance) (adapter.CheckResult, error) {
	h.lock.Lock()
	l := h.list
	err := h.lastFetchError
	h.lock.Unlock()

	if l == nil {
		// no valid list
		return adapter.CheckResult{}, err
	}

	found, err := l.checkList(entry.Value)
	code := rpc.OK
	msg := ""

	if err != nil {
		code = rpc.INVALID_ARGUMENT
		msg = err.Error()
	} else if h.config.Blacklist {
		if found {
			code = rpc.PERMISSION_DENIED
			msg = fmt.Sprintf("%s is blacklisted", entry.Value)
		}
	} else if !found {
		code = rpc.NOT_FOUND
		msg = fmt.Sprintf("%s is not whitelisted", entry.Value)
	}

	return adapter.CheckResult{
		Status:        rpc.Status{Code: int32(code), Message: msg},
		ValidDuration: h.config.CachingInterval,
		ValidUseCount: h.config.CachingUseCount,
	}, nil
}

func (h *handler) Close() error {
	close(h.closing)

	if h.refreshTicker != nil {
		h.refreshTicker.Stop()
		h.purgeTimer.Stop()
	}

	return nil
}

// listRefresher updates the list by polling from the provider on a fixed interval
func (h *handler) listRefresher() {
	for {
		select {
		case <-h.refreshTicker.C:
			h.fetchList()

		case <-h.purgeTimer.C:
			h.purgeList()

		case <-h.closing:
			return
		}
	}
}

// fetchList retrieves and prepares an updated list
//
// TODO: This should implement some more aggressive retry mechanism.
//       Right now, it a fetch fails, the code will just punt and wait
//       until the next refresh timer tick to try again. When a failure
//       happens, the code should switch to a more aggressive incremental
//       backoff approach so that an updated list can be gotten as soon as
//       things are back online.
func (h *handler) fetchList() {
	buf := []byte{}
	sha := h.latestSHA

	var err error

	if h.config.ProviderUrl != "" {
		h.log.Infof("Fetching list from %s", h.config.ProviderUrl)

		var resp *http.Response
		resp, err = http.Get(h.config.ProviderUrl)
		if err != nil || resp.StatusCode != http.StatusOK {
			if err != nil {
				err = h.log.Errorf("could not fetch list from %s: %v", h.config.ProviderUrl, err)
			} else {
				err = h.log.Errorf("could not fetch list from %s: %v", h.config.ProviderUrl, resp.StatusCode)
			}
			h.lock.Lock()
			h.lastFetchError = err
			h.lock.Unlock()
			return
		}

		// TODO: could lead to OOM since this is unbounded
		buf, err = h.readAll(resp.Body)
		if err != nil {
			err = h.log.Errorf("Could not read from %s: %v", h.config.ProviderUrl, err)
			h.lock.Lock()
			h.lastFetchError = err
			h.lock.Unlock()
			return
		}

		// determine whether the list has changed since the last fetch
		sha = sha1.Sum(buf)
		if sha == h.latestSHA && h.list != nil {
			// the list hasn't changed since last time
			h.log.Infof("Fetched list is unchanged")
			h.resetPurgeTimer()
			return
		}
	}

	var l list

	switch h.config.EntryType {
	case config.STRINGS:
		l = parseStringList(buf, h.config.Overrides)
	case config.CASE_INSENSITIVE_STRINGS:
		l = parseCaseInsensitiveStringList(buf, h.config.Overrides)
	case config.IP_ADDRESSES:
		l, err = parseIPList(buf, h.config.Overrides)
		if err != nil {
			err = h.log.Errorf("Could not parse data from %s: %v", h.config.ProviderUrl, err)
			h.lock.Lock()
			h.lastFetchError = err
			h.lock.Unlock()
			return
		}
	}

	// install the new list
	h.log.Infof("Installing updated list with %d entries", l.numEntries())

	h.lock.Lock()
	h.list = l
	h.lastFetchError = nil
	h.lock.Unlock()

	h.latestSHA = sha
	h.resetPurgeTimer()
}

func (h *handler) resetPurgeTimer() {
	if h.purgeTimer == nil {
		return
	}

	// prevent the next purge from happening
	h.purgeTimer.Stop()

	// clean up the channel in case a message is already pending
	select {
	case <-h.purgeTimer.C:
	default:
	}

	// setup the purge timer to clean up the list if it doesn't get refreshed soon enough
	h.purgeTimer.Reset(h.config.Ttl)
}

func (h *handler) purgeList() {
	h.log.Warningf("Purging list due to inability to refresh it in time")

	h.lock.Lock()
	h.list = nil
	h.lock.Unlock()
}

///////////////// Bootstrap ///////////////

// GetBuilderInfo returns the BuilderInfo associated with this adapter implementation.
func GetBuilderInfo() adapter.BuilderInfo {
	return adapter.BuilderInfo{
		Name:               "list-checker",
		Impl:               "istio.io/mixer/adapter/list",
		Description:        "Checks whether an entry is present in a list",
		SupportedTemplates: []string{listentry.TemplateName},
		DefaultConfig: &config.Params{
			ProviderUrl:     "http://localhost",
			RefreshInterval: 60 * time.Second,
			Ttl:             300 * time.Second,
			CachingInterval: 300 * time.Second,
			CachingUseCount: 10000,
			EntryType:       config.STRINGS,
			Blacklist:       false,
		},
		CreateHandlerBuilder: func() adapter.HandlerBuilder { return &builder{} },
		ValidateConfig:       validateConfig,
	}
}

func validateConfig(cfg adapter.Config) (ce *adapter.ConfigErrors) {
	c := cfg.(*config.Params)

	if c.ProviderUrl != "" {
		u, err := url.Parse(c.ProviderUrl)
		if err != nil {
			ce = ce.Append("providerUrl", err)
		} else if u.Scheme == "" || u.Host == "" {
			ce = ce.Appendf("providerUrl", "URL scheme and host cannot be empty")
		}

		if c.RefreshInterval < 1*time.Second {
			ce = ce.Appendf("refreshInterval", "refresh interval must be at least 1 second, it is %v", c.RefreshInterval)
		}

		if c.Ttl < c.RefreshInterval {
			ce = ce.Appendf("ttl", "ttl must be > refreshInterval, ttl is %v and refreshInterval is %v", c.Ttl, c.RefreshInterval)
		}
	}

	if c.CachingInterval < 0 {
		ce = ce.Appendf("cachingInterval", "caching interval must be >= 0, it is %v", c.CachingInterval)
	}

	if c.CachingUseCount < 0 {
		ce = ce.Appendf("cachingUseCount", "caching use count must be >= 0, it is %v", c.CachingUseCount)
	}

	if c.EntryType == config.IP_ADDRESSES {
		for _, ip := range c.Overrides {
			orig := ip
			if !strings.Contains(ip, "/") {
				ip += "/32"
			}

			_, _, err := net.ParseCIDR(ip)
			if err != nil {
				ce = ce.Appendf("overrides", "could not parse override %s: %v", orig, err)
			}
		}
	}

	return
}
