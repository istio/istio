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

package model

import (
	"cmp"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/miekg/dns"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/util/math"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/istiomultierror"
	netutil "istio.io/istio/pkg/util/net"
	"istio.io/istio/pkg/util/sets"
)

// NetworkGateway is the gateway of a network
type NetworkGateway struct {
	// Network is the ID of the network where this Gateway resides.
	Network network.ID
	// Cluster is the ID of the k8s cluster where this Gateway resides.
	Cluster cluster.ID
	// gateway ip address
	Addr string
	// gateway port
	Port uint32
	// HBONEPort if non-zero indicates that the gateway supports HBONE
	HBONEPort uint32
	// ServiceAccount the gateway runs as
	ServiceAccount types.NamespacedName
}

type NetworkGatewaysWatcher interface {
	NetworkGateways() []NetworkGateway
	AppendNetworkGatewayHandler(h func())
}

// NetworkGatewaysHandler can be embedded to easily implement NetworkGatewaysWatcher.
type NetworkGatewaysHandler struct {
	handlers []func()
}

func (ngh *NetworkGatewaysHandler) AppendNetworkGatewayHandler(h func()) {
	ngh.handlers = append(ngh.handlers, h)
}

func (ngh *NetworkGatewaysHandler) NotifyGatewayHandlers() {
	for _, handler := range ngh.handlers {
		handler()
	}
}

type NetworkGateways struct {
	mu *sync.RWMutex
	// least common multiple of gateway number of {per network, per cluster}
	lcm                 uint32
	byNetwork           map[network.ID][]NetworkGateway
	byNetworkAndCluster map[networkAndCluster][]NetworkGateway
}

// NetworkManager provides gateway details for accessing remote networks.
type NetworkManager struct {
	env *Environment
	// exported for test
	NameCache  *networkGatewayNameCache
	xdsUpdater XDSUpdater

	// just to ensure NetworkGateways and Unresolved are updated together
	mu sync.RWMutex
	// embedded NetworkGateways only includes gateways with IPs
	// hostnames are resolved in control plane (or filtered out if feature is disabled)
	*NetworkGateways
	// includes all gateways with no DNS resolution or filtering, regardless of feature flags
	Unresolved *NetworkGateways
}

// NewNetworkManager creates a new NetworkManager from the Environment by merging
// together the MeshNetworks and ServiceRegistry-specific gateways.
func NewNetworkManager(env *Environment, xdsUpdater XDSUpdater) (*NetworkManager, error) {
	nameCache, err := newNetworkGatewayNameCache()
	if err != nil {
		return nil, err
	}
	mgr := &NetworkManager{
		env:             env,
		NameCache:       nameCache,
		xdsUpdater:      xdsUpdater,
		NetworkGateways: &NetworkGateways{},
		Unresolved:      &NetworkGateways{},
	}

	// share lock with root NetworkManager
	mgr.NetworkGateways.mu = &mgr.mu
	mgr.Unresolved.mu = &mgr.mu

	env.AddNetworksHandler(mgr.reloadGateways)
	// register to per registry, will be called when gateway service changed
	env.AppendNetworkGatewayHandler(mgr.reloadGateways)
	nameCache.AppendNetworkGatewayHandler(mgr.reloadGateways)
	mgr.reload()
	return mgr, nil
}

// reloadGateways reloads NetworkGateways and triggers a push if they change.
func (mgr *NetworkManager) reloadGateways() {
	changed := mgr.reload()

	if changed && mgr.xdsUpdater != nil {
		log.Infof("gateways changed, triggering push")
		mgr.xdsUpdater.ConfigUpdate(&PushRequest{Full: true, Reason: NewReasonStats(NetworksTrigger), Forced: true})
	}
}

func (mgr *NetworkManager) reload() bool {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	log.Infof("reloading network gateways")

	// Generate a snapshot of the state of gateways by merging the contents of
	// MeshNetworks and the ServiceRegistries.

	// Store all gateways in a set initially to eliminate duplicates.
	gatewaySet := make(NetworkGatewaySet)

	// First, load gateways from the static MeshNetworks config.
	meshNetworks := mgr.env.NetworksWatcher.Networks()
	if meshNetworks != nil {
		for nw, networkConf := range meshNetworks.Networks {
			for _, gw := range networkConf.Gateways {
				if gw.GetAddress() == "" {
					// registryServiceName addresses will be populated via kube service registry
					continue
				}
				gatewaySet.Insert(NetworkGateway{
					Cluster: "", /* TODO(nmittler): Add Cluster to the API */
					Network: network.ID(nw),
					Addr:    gw.GetAddress(),
					Port:    gw.Port,
				})
			}
		}
	}

	// Second, load registry-specific gateways.
	// - the internal map of label gateways - these get deleted if the service is deleted, updated if the ip changes etc.
	// - the computed map from meshNetworks (triggered by reloadNetworkLookup, the ported logic from getGatewayAddresses)
	gatewaySet.InsertAll(mgr.env.NetworkGateways()...)
	resolvedGatewaySet := mgr.resolveHostnameGateways(gatewaySet)

	return mgr.NetworkGateways.update(resolvedGatewaySet) || mgr.Unresolved.update(gatewaySet)
}

// update calls should with the lock held
func (gws *NetworkGateways) update(gatewaySet NetworkGatewaySet) bool {
	if gatewaySet.Equals(sets.New(gws.allGateways()...)) {
		return false
	}

	// index by network or network+cluster for quick lookup
	byNetwork := make(map[network.ID][]NetworkGateway)
	byNetworkAndCluster := make(map[networkAndCluster][]NetworkGateway)
	for gw := range gatewaySet {
		byNetwork[gw.Network] = append(byNetwork[gw.Network], gw)
		nc := networkAndClusterForGateway(&gw)
		byNetworkAndCluster[nc] = append(byNetworkAndCluster[nc], gw)
	}

	var gwNum []int
	// Sort the gateways in byNetwork, and also calculate the max number
	// of gateways per network.
	for k, gws := range byNetwork {
		byNetwork[k] = SortGateways(gws)
		gwNum = append(gwNum, len(gws))
	}

	// Sort the gateways in byNetworkAndCluster.
	for k, gws := range byNetworkAndCluster {
		byNetworkAndCluster[k] = SortGateways(gws)
		gwNum = append(gwNum, len(gws))
	}

	lcmVal := 1
	// calculate lcm
	for _, num := range gwNum {
		lcmVal = math.Lcm(lcmVal, num)
	}

	gws.lcm = uint32(lcmVal)
	gws.byNetwork = byNetwork
	gws.byNetworkAndCluster = byNetworkAndCluster

	return true
}

// resolveHostnameGateway either resolves or removes gateways that use a non-IP Address
func (mgr *NetworkManager) resolveHostnameGateways(gatewaySet NetworkGatewaySet) NetworkGatewaySet {
	resolvedGatewaySet := make(NetworkGatewaySet, len(gatewaySet))
	// filter the list of gateways to resolve
	hostnameGateways := map[string][]NetworkGateway{}
	names := sets.New[string]()
	for gw := range gatewaySet {
		if netutil.IsValidIPAddress(gw.Addr) {
			resolvedGatewaySet.Insert(gw)
			continue
		}
		if !features.ResolveHostnameGateways {
			log.Warnf("Failed parsing gateway address %s from Service Registry. "+
				"Set RESOLVE_HOSTNAME_GATEWAYS on istiod to enable resolving hostnames in the control plane.",
				gw.Addr)
			continue
		}
		hostnameGateways[gw.Addr] = append(hostnameGateways[gw.Addr], gw)
		names.Insert(gw.Addr)
	}

	if !features.ResolveHostnameGateways {
		return resolvedGatewaySet
	}
	// resolve each hostname
	for host, addrs := range mgr.NameCache.Resolve(names) {
		gwsForHost := hostnameGateways[host]
		if len(addrs) == 0 {
			log.Warnf("could not resolve hostname %q for %d gateways", host, len(gwsForHost))
		}
		// expand each resolved address into a NetworkGateway
		for _, gw := range gwsForHost {
			for _, resolved := range addrs {
				// copy the base gateway to preserve the port/network, but update with the resolved IP
				resolvedGw := gw
				resolvedGw.Addr = resolved
				resolvedGatewaySet.Insert(resolvedGw)
			}
		}
	}
	return resolvedGatewaySet
}

func (gws *NetworkGateways) IsMultiNetworkEnabled() bool {
	if gws == nil {
		return false
	}
	gws.mu.RLock()
	defer gws.mu.RUnlock()
	return len(gws.byNetwork) > 0
}

// GetLBWeightScaleFactor returns the least common multiple of the number of gateways per network.
func (gws *NetworkGateways) GetLBWeightScaleFactor() uint32 {
	gws.mu.RLock()
	defer gws.mu.RUnlock()
	return gws.lcm
}

func (gws *NetworkGateways) AllGateways() []NetworkGateway {
	gws.mu.RLock()
	defer gws.mu.RUnlock()
	return gws.allGateways()
}

func (gws *NetworkGateways) allGateways() []NetworkGateway {
	if gws.byNetwork == nil {
		return nil
	}
	out := make([]NetworkGateway, 0)
	for _, gateways := range gws.byNetwork {
		out = append(out, gateways...)
	}
	return SortGateways(out)
}

func (gws *NetworkGateways) GatewaysForNetwork(nw network.ID) []NetworkGateway {
	gws.mu.RLock()
	defer gws.mu.RUnlock()
	if gws.byNetwork == nil {
		return nil
	}
	return gws.byNetwork[nw]
}

func (gws *NetworkGateways) GatewaysForNetworkAndCluster(nw network.ID, c cluster.ID) []NetworkGateway {
	gws.mu.RLock()
	defer gws.mu.RUnlock()
	if gws.byNetworkAndCluster == nil {
		return nil
	}
	return gws.byNetworkAndCluster[networkAndClusterFor(nw, c)]
}

type networkAndCluster struct {
	network network.ID
	cluster cluster.ID
}

func networkAndClusterForGateway(g *NetworkGateway) networkAndCluster {
	return networkAndClusterFor(g.Network, g.Cluster)
}

func networkAndClusterFor(nw network.ID, c cluster.ID) networkAndCluster {
	return networkAndCluster{
		network: nw,
		cluster: c,
	}
}

// SortGateways sorts the array so that it's stable.
func SortGateways(gws []NetworkGateway) []NetworkGateway {
	return slices.SortFunc(gws, func(a, b NetworkGateway) int {
		if r := cmp.Compare(a.Addr, b.Addr); r != 0 {
			return r
		}
		return cmp.Compare(a.Port, b.Port)
	})
}

// NetworkGatewaySet is a helper to manage a set of NetworkGateway instances.
type NetworkGatewaySet = sets.Set[NetworkGateway]

var (
	// MinGatewayTTL is exported for testing
	MinGatewayTTL = 30 * time.Second

	// https://github.com/coredns/coredns/blob/v1.10.1/plugin/pkg/dnsutil/ttl.go#L51
	MaxGatewayTTL = 1 * time.Hour
)

type networkGatewayNameCache struct {
	NetworkGatewaysHandler
	client *dnsClient

	sync.Mutex
	cache map[string]nameCacheEntry
}

type nameCacheEntry struct {
	value  []string
	expiry time.Time
	timer  *time.Timer
}

func newNetworkGatewayNameCache() (*networkGatewayNameCache, error) {
	c, err := newClient()
	if err != nil {
		return nil, err
	}
	return newNetworkGatewayNameCacheWithClient(c), nil
}

// newNetworkGatewayNameCacheWithClient exported for test
func newNetworkGatewayNameCacheWithClient(c *dnsClient) *networkGatewayNameCache {
	return &networkGatewayNameCache{client: c, cache: map[string]nameCacheEntry{}}
}

// Resolve takes a list of hostnames and returns a map of names to addresses
func (n *networkGatewayNameCache) Resolve(names sets.String) map[string][]string {
	n.Lock()
	defer n.Unlock()

	n.cleanupWatches(names)

	out := make(map[string][]string, len(names))
	for name := range names {
		out[name] = n.resolveFromCache(name)
	}

	return out
}

// cleanupWatches cancels any scheduled re-resolve for names we no longer care about
func (n *networkGatewayNameCache) cleanupWatches(names sets.String) {
	for name, entry := range n.cache {
		if names.Contains(name) {
			continue
		}
		entry.timer.Stop()
		delete(n.cache, name)
	}
}

func (n *networkGatewayNameCache) resolveFromCache(name string) []string {
	if entry, ok := n.cache[name]; ok && entry.expiry.After(time.Now()) {
		return entry.value
	}
	// ideally this will not happen more than once for each name and the cache auto-updates in the background
	// even if it does, this happens on the SotW ingestion path (kube or meshnetworks changes) and not xds push path.
	return n.resolveAndCache(name)
}

func (n *networkGatewayNameCache) resolveAndCache(name string) []string {
	entry, ok := n.cache[name]
	if ok {
		entry.timer.Stop()
	}
	delete(n.cache, name)
	addrs, ttl, err := n.resolve(name)
	// avoid excessive pushes due to small TTL
	if ttl < MinGatewayTTL {
		ttl = MinGatewayTTL
	}
	expiry := time.Now().Add(ttl)
	if err != nil {
		// gracefully retain old addresses in case the DNS server is unavailable
		addrs = entry.value
	}
	n.cache[name] = nameCacheEntry{
		value:  addrs,
		expiry: expiry,
		// TTL expires, try to refresh TODO should this be < ttl?
		timer: time.AfterFunc(ttl, n.refreshAndNotify(name)),
	}

	return addrs
}

// refreshAndNotify is triggered via time.AfterFunc and will recursively schedule itself that way until timer is cleaned
// up via cleanupWatches.
func (n *networkGatewayNameCache) refreshAndNotify(name string) func() {
	return func() {
		log.Debugf("network gateways: refreshing DNS for %s", name)
		n.Lock()
		old := n.cache[name]
		addrs := n.resolveAndCache(name)
		n.Unlock()

		if !slices.Equal(old.value, addrs) {
			log.Debugf("network gateways: DNS for %s changed: %v -> %v", name, old.value, addrs)
			n.NotifyGatewayHandlers()
		}
	}
}

// resolve gets all the A and AAAA records for the given name
func (n *networkGatewayNameCache) resolve(name string) ([]string, time.Duration, error) {
	ttl := MaxGatewayTTL
	var out []string
	errs := istiomultierror.New()

	var mu sync.Mutex
	var wg sync.WaitGroup
	doResolve := func(dnsType uint16) {
		defer wg.Done()

		res := n.client.Query(new(dns.Msg).SetQuestion(dns.Fqdn(name), dnsType))

		mu.Lock()
		defer mu.Unlock()
		if res.Rcode == dns.RcodeServerFailure {
			errs = multierror.Append(errs, fmt.Errorf("upstream dns failure, qtype: %v", dnsType))
			return
		}
		for _, rr := range res.Answer {
			switch record := rr.(type) {
			case *dns.A:
				out = append(out, record.A.String())
			case *dns.AAAA:
				out = append(out, record.AAAA.String())
			}
		}
		if nextTTL := minimalTTL(res); nextTTL < ttl {
			ttl = nextTTL
		}
	}

	wg.Add(2)
	go doResolve(dns.TypeA)
	go doResolve(dns.TypeAAAA)
	wg.Wait()

	sort.Strings(out)
	if errs.Len() == 2 {
		// return error only if all requests are failed
		return out, MinGatewayTTL, errs
	}
	return out, ttl, nil
}

// https://github.com/coredns/coredns/blob/v1.10.1/plugin/pkg/dnsutil/ttl.go
func minimalTTL(m *dns.Msg) time.Duration {
	// No records or OPT is the only record, return a short ttl as a fail safe.
	if len(m.Answer)+len(m.Ns) == 0 &&
		(len(m.Extra) == 0 || (len(m.Extra) == 1 && m.Extra[0].Header().Rrtype == dns.TypeOPT)) {
		return MinGatewayTTL
	}

	minTTL := MaxGatewayTTL
	for _, r := range m.Answer {
		if r.Header().Ttl < uint32(minTTL.Seconds()) {
			minTTL = time.Duration(r.Header().Ttl) * time.Second
		}
	}
	for _, r := range m.Ns {
		if r.Header().Ttl < uint32(minTTL.Seconds()) {
			minTTL = time.Duration(r.Header().Ttl) * time.Second
		}
	}

	for _, r := range m.Extra {
		if r.Header().Rrtype == dns.TypeOPT {
			// OPT records use TTL field for extended rcode and flags
			continue
		}
		if r.Header().Ttl < uint32(minTTL.Seconds()) {
			minTTL = time.Duration(r.Header().Ttl) * time.Second
		}
	}
	return minTTL
}

// TODO share code with pkg/dns
type dnsClient struct {
	*dns.Client
	resolvConfServers []string
}

// NetworkGatewayTestDNSServers if set will ignore resolv.conf and use the given DNS servers for tests.
var NetworkGatewayTestDNSServers []string

func newClient() (*dnsClient, error) {
	servers := NetworkGatewayTestDNSServers
	if len(servers) == 0 {
		dnsConfig, err := dns.ClientConfigFromFile("/etc/resolv.conf")
		if err != nil {
			return nil, err
		}
		if dnsConfig != nil {
			for _, s := range dnsConfig.Servers {
				servers = append(servers, net.JoinHostPort(s, dnsConfig.Port))
			}
		}
		// TODO take search namespaces into account
		// TODO what about /etc/hosts?
	}

	c := &dnsClient{
		Client: &dns.Client{
			DialTimeout:  5 * time.Second,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
		},
	}
	c.resolvConfServers = append(c.resolvConfServers, servers...)
	return c, nil
}

// for more informative logging of dns errors
func getReqNames(req *dns.Msg) []string {
	names := make([]string, 0, 1)
	for _, qq := range req.Question {
		names = append(names, qq.Name)
	}
	return names
}

func (c *dnsClient) Query(req *dns.Msg) *dns.Msg {
	var response *dns.Msg
	for _, upstream := range c.resolvConfServers {
		cResponse, _, err := c.Exchange(req, upstream)
		rcode := dns.RcodeServerFailure
		if err == nil && cResponse != nil {
			rcode = cResponse.Rcode
		}
		if rcode == dns.RcodeServerFailure {
			// RcodeServerFailure means the upstream cannot serve the request
			// https://github.com/coredns/coredns/blob/v1.10.1/plugin/forward/forward.go#L193
			log.Infof("upstream dns failure: %v: %v: %v", upstream, getReqNames(req), err)
			continue
		}
		response = cResponse
		if rcode == dns.RcodeSuccess {
			break
		}
		codeString := dns.RcodeToString[rcode]
		log.Debugf("upstream dns error: %v: %v: %v", upstream, getReqNames(req), codeString)
	}
	if response == nil {
		response = new(dns.Msg)
		response.SetReply(req)
		response.Rcode = dns.RcodeServerFailure
	}
	return response
}
