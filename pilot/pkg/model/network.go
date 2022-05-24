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
	"math"
	"net"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/network"
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

// NewNetworkManager creates a new NetworkManager from the Environment by merging
// together the MeshNetworks and ServiceRegistry-specific gateways.
func NewNetworkManager(env *Environment, xdsUpdater XDSUpdater) (*NetworkManager, error) {
	nameCache, err := newNetworkGatewayNameCache()
	if err != nil {
		return nil, err
	}
	mgr := &NetworkManager{env: env, NameCache: nameCache, xdsUpdater: xdsUpdater}
	env.AddNetworksHandler(mgr.reloadAndPush)
	env.AppendNetworkGatewayHandler(mgr.reloadAndPush)
	nameCache.AppendNetworkGatewayHandler(mgr.reloadAndPush)
	mgr.reload()
	return mgr, nil
}

func (mgr *NetworkManager) reloadAndPush() {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	oldGateways := make(NetworkGatewaySet)
	for _, gateway := range mgr.allGateways() {
		oldGateways.Add(gateway)
	}
	changed := !mgr.reload().Equals(oldGateways)

	if changed && mgr.xdsUpdater != nil {
		log.Infof("gateways changed, triggering push")
		mgr.xdsUpdater.ConfigUpdate(&PushRequest{Full: true, Reason: []TriggerReason{NetworksTrigger}})
	}
}

func (mgr *NetworkManager) reload() NetworkGatewaySet {
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
				gatewaySet[NetworkGateway{
					Cluster: "", /* TODO(nmittler): Add Cluster to the API */
					Network: network.ID(nw),
					Addr:    gw.GetAddress(),
					Port:    gw.Port,
				}] = struct{}{}
			}
		}
	}

	// Second, load registry-specific gateways.
	for _, gw := range mgr.env.NetworkGateways() {
		// - the internal map of label gateways - these get deleted if the service is deleted, updated if the ip changes etc.
		// - the computed map from meshNetworks (triggered by reloadNetworkLookup, the ported logic from getGatewayAddresses)
		gatewaySet[gw] = struct{}{}
	}

	mgr.resolveHostnameGateways(gatewaySet)

	// Now populate the maps by network and by network+cluster.
	byNetwork := make(map[network.ID][]NetworkGateway)
	byNetworkAndCluster := make(map[networkAndCluster][]NetworkGateway)
	for gw := range gatewaySet {
		byNetwork[gw.Network] = append(byNetwork[gw.Network], gw)
		nc := networkAndClusterForGateway(&gw)
		byNetworkAndCluster[nc] = append(byNetworkAndCluster[nc], gw)
	}

	gwNum := []int{}
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
		lcmVal = lcm(lcmVal, num)
	}

	mgr.lcm = uint32(lcmVal)
	mgr.byNetwork = byNetwork
	mgr.byNetworkAndCluster = byNetworkAndCluster

	return gatewaySet
}

func (mgr *NetworkManager) resolveHostnameGateways(gatewaySet map[NetworkGateway]struct{}) {
	// filter the list of gateways to resolve
	hostnameGateways := map[string][]NetworkGateway{}
	names := sets.New()
	for gw := range gatewaySet {
		if gwIP := net.ParseIP(gw.Addr); gwIP != nil {
			continue
		}
		delete(gatewaySet, gw)
		if !features.ResolveHostnameGateways {
			log.Warnf("Failed parsing gateway address %s from Service Registry. "+
				"Set RESOLVE_HOSTNAME_GATEWAYS on istiod to enable resolving hostnames in the control plane.",
				gw.Addr)
			continue
		}
		hostnameGateways[gw.Addr] = append(hostnameGateways[gw.Addr], gw)
		names.Insert(gw.Addr)
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
				gatewaySet[resolvedGw] = struct{}{}
			}
		}
	}
}

// NetworkManager provides gateway details for accessing remote networks.
type NetworkManager struct {
	env *Environment
	// exported for test
	NameCache  *networkGatewayNameCache
	xdsUpdater XDSUpdater

	// least common multiple of gateway number of {per network, per cluster}
	mu                  sync.RWMutex
	lcm                 uint32
	byNetwork           map[network.ID][]NetworkGateway
	byNetworkAndCluster map[networkAndCluster][]NetworkGateway
}

func (mgr *NetworkManager) IsMultiNetworkEnabled() bool {
	if mgr == nil {
		return false
	}
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	return len(mgr.byNetwork) > 0
}

// GetLBWeightScaleFactor returns the least common multiple of the number of gateways per network.
func (mgr *NetworkManager) GetLBWeightScaleFactor() uint32 {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	return mgr.lcm
}

func (mgr *NetworkManager) AllGateways() []NetworkGateway {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	return mgr.allGateways()
}

func (mgr *NetworkManager) allGateways() []NetworkGateway {
	if mgr.byNetwork == nil {
		return nil
	}
	out := make([]NetworkGateway, 0)
	for _, gateways := range mgr.byNetwork {
		out = append(out, gateways...)
	}
	return SortGateways(out)
}

func (mgr *NetworkManager) GatewaysByNetwork() map[network.ID][]NetworkGateway {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	if mgr.byNetwork == nil {
		return nil
	}
	out := make(map[network.ID][]NetworkGateway)
	for k, v := range mgr.byNetwork {
		out[k] = append(make([]NetworkGateway, 0, len(v)), v...)
	}
	return out
}

func (mgr *NetworkManager) GatewaysForNetwork(nw network.ID) []NetworkGateway {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	if mgr.byNetwork == nil {
		return nil
	}
	return mgr.byNetwork[nw]
}

func (mgr *NetworkManager) GatewaysForNetworkAndCluster(nw network.ID, c cluster.ID) []NetworkGateway {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	if mgr.byNetwork == nil {
		return nil
	}
	return mgr.byNetworkAndCluster[networkAndClusterFor(nw, c)]
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

func SortGateways(gws []NetworkGateway) []NetworkGateway {
	// Sort the array so that it's stable.
	sort.SliceStable(gws, func(i, j int) bool {
		if cmp := strings.Compare(gws[i].Addr, gws[j].Addr); cmp < 0 {
			return true
		}
		return gws[i].Port < gws[j].Port
	})
	return gws
}

// greatest common divisor of x and y
func gcd(x, y int) int {
	var tmp int
	for {
		tmp = x % y
		if tmp > 0 {
			x = y
			y = tmp
		} else {
			return y
		}
	}
}

// least common multiple of x and y
func lcm(x, y int) int {
	return x * y / gcd(x, y)
}

// NetworkGatewaySet is a helper to manage a set of NetworkGateway instances.
type NetworkGatewaySet map[NetworkGateway]struct{}

func (s NetworkGatewaySet) Equals(other NetworkGatewaySet) bool {
	if len(s) != len(other) {
		return false
	}
	// deepequal won't catch nil-map == empty map
	if len(s) == 0 && len(other) == 0 {
		return true
	}
	return reflect.DeepEqual(s, other)
}

func (s NetworkGatewaySet) Add(gw NetworkGateway) {
	s[gw] = struct{}{}
}

func (s NetworkGatewaySet) AddAll(other NetworkGatewaySet) {
	for gw := range other {
		s.Add(gw)
	}
}

func (s NetworkGatewaySet) ToArray() []NetworkGateway {
	gws := make([]NetworkGateway, 0, len(s))
	for gw := range s {
		gws = append(gws, gw)
	}

	// Sort the array so that it's stable.
	gws = SortGateways(gws)
	return gws
}

// MinGatewayTTL is exported for testing
var MinGatewayTTL = 30 * time.Second

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
func (n *networkGatewayNameCache) Resolve(names sets.Set) map[string][]string {
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
func (n *networkGatewayNameCache) cleanupWatches(names sets.Set) {
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
	if entry, ok := n.cache[name]; ok {
		entry.timer.Stop()
	}
	delete(n.cache, name)
	addrs, ttl := n.resolve(name)
	// avoid excessive pushes due to small TTL
	if ttl < MinGatewayTTL {
		ttl = MinGatewayTTL
	}
	expiry := time.Now().Add(ttl)
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

		if !stringSliceEqual(old.value, addrs) {
			log.Debugf("network gateways: DNS for %s changed: %v -> %v", name, old.value, addrs)
			n.NotifyGatewayHandlers()
		}
	}
}

// avoid import cycle
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

// resolve gets all the A and AAAA records for the given name
func (n *networkGatewayNameCache) resolve(name string) ([]string, time.Duration) {
	// TODO figure out how to query only A + AAAA
	res := n.client.Query(new(dns.Msg).SetQuestion(dns.Fqdn(name), dns.TypeANY))
	if res == nil || len(res.Answer) == 0 {
		return nil, 0
	}
	ttl := uint32(math.MaxUint32)
	var out []string
	for _, rr := range res.Answer {
		switch v := rr.(type) {
		case *dns.A:
			out = append(out, v.A.String())
		case *dns.AAAA:
			// TODO may not always want ipv6t?
			out = append(out, v.AAAA.String())
		default:
			// not a valid record, don't inspect TTL
			continue
		}
		if nextTTL := rr.Header().Ttl; nextTTL < ttl {
			ttl = nextTTL
		}
	}
	sort.Strings(out)
	return out, time.Duration(ttl)
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

func (c *dnsClient) Query(req *dns.Msg) *dns.Msg {
	var response *dns.Msg
	for _, upstream := range c.resolvConfServers {
		cResponse, _, err := c.Exchange(req, upstream)
		if err == nil {
			response = cResponse
			break
		}
		log.Infof("upstream dns failure: %v", err)
	}
	if response == nil {
		response = new(dns.Msg)
		response.SetReply(req)
		response.Rcode = dns.RcodeServerFailure
	}
	return response
}
