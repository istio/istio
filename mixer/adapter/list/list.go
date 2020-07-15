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

// nolint: lll
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -a mixer/adapter/list/config/config.proto -x "-n listchecker -t listentry -d example"

// Package list provides an adapter that implements the listEntry
// template to enable blacklist / whitelist checking of values.
package list // import "istio.io/istio/mixer/adapter/list"

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/adapter/list/config"
	"istio.io/istio/mixer/adapter/metadata"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/istio/mixer/template/listentry"
)

type (
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

	var value string

	switch entryVal := entry.Value.(type) {
	case *v1beta1.Value:
		strVal := entryVal.GetStringValue()
		ipVal := entryVal.GetIpAddressValue()
		if strVal != "" {
			value = strVal
		} else if ipVal != nil {
			// IP_ADDRESS comes in as byte array.
			value = net.IP(ipVal.Value).String()
		} else {
			return getCheckResult(h.config, rpc.INVALID_ARGUMENT, fmt.Sprintf("%v is not a valid string or IP address", entryVal)), nil
		}
	case string:
		value = entryVal
	case []byte:
		value = net.IP(entryVal).String()
	default:
		return getCheckResult(h.config, rpc.INVALID_ARGUMENT, fmt.Sprintf("%v is not a valid string or IP address", entryVal)), nil
	}
	found, err := l.checkList(value)
	code := rpc.OK
	msg := ""

	if err != nil {
		code = rpc.INVALID_ARGUMENT
		msg = err.Error()
	} else if h.config.Blacklist {
		if found {
			code = rpc.PERMISSION_DENIED
			msg = fmt.Sprintf("%s is blacklisted", value)
		}
	} else if !found {
		code = rpc.PERMISSION_DENIED
		msg = fmt.Sprintf("%s is not whitelisted", value)
	}

	return getCheckResult(h.config, code, msg), nil
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
	case config.REGEX:
		l, err = parseRegexList(buf, h.config.Overrides)
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

func (h *handler) hasData() (bool, error) {
	h.lock.Lock()
	result := h.list != nil
	err := h.lastFetchError
	h.lock.Unlock()

	return result, err
}

func getCheckResult(config config.Params, code rpc.Code, msg string) adapter.CheckResult {
	return adapter.CheckResult{
		Status:        status.WithMessage(code, msg),
		ValidDuration: config.CachingInterval,
		ValidUseCount: config.CachingUseCount,
	}
}

///////////////// Bootstrap ///////////////

// GetInfo returns the Info associated with this adapter implementation.
func GetInfo() adapter.Info {
	info := metadata.GetInfo("listchecker")
	info.NewBuilder = func() adapter.HandlerBuilder { return &builder{} }
	return info
}

type builder struct {
	adapterConfig *config.Params
}

func (*builder) SetListEntryTypes(map[string]*listentry.Type) {}
func (b *builder) SetAdapterConfig(cfg adapter.Config)        { b.adapterConfig = cfg.(*config.Params) }

func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	ac := b.adapterConfig

	if ac.ProviderUrl != "" {
		u, err := url.Parse(ac.ProviderUrl)
		if err != nil {
			ce = ce.Append("providerUrl", err)
		} else if u.Scheme == "" || u.Host == "" {
			ce = ce.Appendf("providerUrl", "URL scheme and host cannot be empty")
		}

		if ac.RefreshInterval < 1*time.Second {
			ce = ce.Appendf("refreshInterval", "refresh interval must be at least 1 second, it is %v", ac.RefreshInterval)
		}

		if ac.Ttl < ac.RefreshInterval {
			ce = ce.Appendf("ttl", "ttl must be > refreshInterval, ttl is %v and refreshInterval is %v", ac.Ttl, ac.RefreshInterval)
		}
	}

	if ac.CachingInterval < 0 {
		ce = ce.Appendf("cachingInterval", "caching interval must be >= 0, it is %v", ac.CachingInterval)
	}

	if ac.CachingUseCount < 0 {
		ce = ce.Appendf("cachingUseCount", "caching use count must be >= 0, it is %v", ac.CachingUseCount)
	}

	if ac.EntryType == config.IP_ADDRESSES {
		for _, ip := range ac.Overrides {
			orig := ip
			if !strings.Contains(ip, "/") {
				ip += "/32"
			}

			_, _, err := net.ParseCIDR(ip)
			if err != nil {
				ce = ce.Appendf("overrides", "could not parse ip address override %s: %v", orig, err)
			}
		}
	}

	if ac.EntryType == config.REGEX {
		for _, regex := range ac.Overrides {
			if _, err := regexp.Compile(regex); err != nil {
				ce = ce.Appendf("overrides", "could not parse regex override %s: %v", regex, err)
			}
		}
	}

	return
}

func (b *builder) Build(context context.Context, env adapter.Env) (adapter.Handler, error) {
	ac := b.adapterConfig

	h := &handler{
		log:     env.Logger(),
		closing: make(chan bool),
		config:  *ac,
		readAll: ioutil.ReadAll,
	}

	if ac.ProviderUrl != "" {
		h.refreshTicker = time.NewTicker(ac.RefreshInterval)
		h.purgeTimer = time.NewTimer(ac.Ttl)
	}

	// Load up the list synchronously so we're ready to accept traffic immediately.
	h.fetchList()

	if ac.ProviderUrl != "" {
		// goroutine to periodically refresh the list
		env.ScheduleDaemon(h.listRefresher)
	}

	return h, nil
}
