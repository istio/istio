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

package envoy

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"sort"
	"time"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/model"
	"istio.io/pilot/proxy"
)

const (
	ingressNode = "ingress"
	certFile    = "/etc/tls.crt"
	keyFile     = "/etc/tls.key"
)

type ingressWatcher struct {
	agent   proxy.Agent
	secrets model.SecretRegistry
	mesh    *proxyconfig.ProxyMeshConfig
	tls     *model.TLSSecret
}

// NewIngressWatcher creates a new ingress watcher instance with an agent
func NewIngressWatcher(mesh *proxyconfig.ProxyMeshConfig, secrets model.SecretRegistry) (Watcher, error) {
	if mesh.StatsdUdpAddress != "" {
		if addr, err := resolveStatsdAddr(mesh.StatsdUdpAddress); err == nil {
			mesh.StatsdUdpAddress = addr
		} else {
			glog.Warningf("Error resolving statsd address; clearing to prevent bad config: %v", err)
			mesh.StatsdUdpAddress = ""
		}
	}
	agent := proxy.NewAgent(runEnvoy(mesh, ingressNode), proxy.DefaultRetry)
	out := &ingressWatcher{
		agent:   agent,
		secrets: secrets,
		mesh:    mesh,
	}
	return out, nil
}

func (w *ingressWatcher) Run(stop <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	go w.agent.Run(stop)
	go func() {
		<-stop
		glog.V(2).Info("Ingress watcher terminating...")
		cancel()
	}()

	client := &http.Client{Timeout: convertDuration(w.mesh.ConnectTimeout)}
	url := fmt.Sprintf("http://%s/v1alpha/secret/%s/%s",
		w.mesh.DiscoveryAddress, w.mesh.IstioServiceCluster, ingressNode)

	config := generateIngress(w.mesh, nil, certFile, keyFile)
	w.agent.ScheduleConfigUpdate(config)

	if w.mesh.AuthPolicy == proxyconfig.ProxyMeshConfig_MUTUAL_TLS {
		go watchCerts(w.mesh.AuthCertsPath, stop, func() {
			c := generateIngress(w.mesh, w.tls, certFile, keyFile)
			w.agent.ScheduleConfigUpdate(c)
		})
	}

	for {
		tls, err := fetchSecret(ctx, client, url, w.secrets)
		if err != nil {
			glog.Warning(err)
		} else {
			w.tls = tls
			config = generateIngress(w.mesh, tls, certFile, keyFile)
			w.agent.ScheduleConfigUpdate(config)
		}

		select {
		case <-time.After(convertDuration(w.mesh.DiscoveryRefreshDelay)):
			// try again
		case <-ctx.Done():
			return
		}
	}
}

// fetchSecret fetches a TLS secret from discovery and secret storage
func fetchSecret(ctx context.Context, client *http.Client, url string,
	secrets model.SecretRegistry) (*model.TLSSecret, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, multierror.Prefix(err, "failed to create a request to "+url)
	}
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return nil, multierror.Prefix(err, "failed to fetch "+url)
	}
	uri, err := ioutil.ReadAll(resp.Body)
	defer resp.Body.Close() // nolint: errcheck
	if err != nil {
		return nil, multierror.Prefix(err, "failed to read request body")
	}
	secret := string(uri)
	if secret == "" {
		glog.V(4).Info("no secret needed")
		return nil, nil
	}
	out, err := secrets.GetTLSSecret(secret)
	if err != nil {
		return nil, multierror.Prefix(err, "failed to read secret from storage")
	}
	return out, nil
}

// generateIngress generates ingress proxy configuration
func generateIngress(mesh *proxyconfig.ProxyMeshConfig, tls *model.TLSSecret, certFile, keyFile string) *Config {
	listeners := []*Listener{
		buildHTTPListener(mesh, nil, WildcardAddress, 80, true, true),
	}

	if tls != nil {
		if err := writeTLS(certFile, keyFile, tls); err != nil {
			glog.Warning("Failed to write cert/key")
		} else {
			listener := buildHTTPListener(mesh, nil, WildcardAddress, 443, true, true)
			listener.SSLContext = &SSLContext{
				CertChainFile:  certFile,
				PrivateKeyFile: keyFile,
			}
			listeners = append(listeners, listener)
		}
	}

	config := buildConfig(listeners, nil, mesh)

	h := sha256.New()
	hashed := false
	if tls != nil {
		hashed = true
		if _, err := h.Write(tls.Certificate); err != nil {
			glog.Warning(err)
		}
		if _, err := h.Write(tls.PrivateKey); err != nil {
			glog.Warning(err)
		}
	}

	if mesh.AuthPolicy == proxyconfig.ProxyMeshConfig_MUTUAL_TLS {
		hashed = true
		if _, err := h.Write(generateCertHash(mesh.AuthCertsPath)); err != nil {
			glog.Warning(err)
		}
	}

	if hashed {
		config.Hash = h.Sum(nil)
	}

	return config
}

func writeTLS(certFile, keyFile string, tls *model.TLSSecret) error {
	if err := ioutil.WriteFile(certFile, tls.Certificate, 0755); err != nil {
		return err
	}
	if err := ioutil.WriteFile(keyFile, tls.PrivateKey, 0755); err != nil {
		return err
	}

	return nil
}

func buildIngressRoutes(ingressRules map[string]*proxyconfig.IngressRule,
	discovery model.ServiceDiscovery,
	config model.IstioConfigStore) (HTTPRouteConfigs, string) {
	// build vhosts
	vhosts := make(map[string][]*HTTPRoute)
	vhostsTLS := make(map[string][]*HTTPRoute)
	tlsAll := ""

	// skip over source-matched route rules
	rules := config.RouteRulesBySource(nil)

	for _, rule := range ingressRules {
		routes, tls, err := buildIngressRoute(rule, discovery, rules)
		if err != nil {
			glog.Warningf("Error constructing Envoy route from ingress rule: %v", err)
			continue
		}

		host := "*"
		if rule.Match != nil {
			if authority, ok := rule.Match.HttpHeaders[model.HeaderAuthority]; ok {
				switch match := authority.GetMatchType().(type) {
				case *proxyconfig.StringMatch_Exact:
					host = match.Exact
				default:
					glog.Warningf("Unsupported match type for authority condition %T, falling back to %q", match, host)
					continue
				}
			}
		}
		if tls != "" {
			vhostsTLS[host] = append(vhostsTLS[host], routes...)
			if tlsAll == "" {
				tlsAll = tls
			} else if tlsAll != tls {
				glog.Warningf("Multiple secrets detected %s and %s", tls, tlsAll)
				if tls < tlsAll {
					tlsAll = tls
				}
			}
		} else {
			vhosts[host] = append(vhosts[host], routes...)
		}
	}

	// normalize config
	rc := &HTTPRouteConfig{VirtualHosts: make([]*VirtualHost, 0)}
	for host, routes := range vhosts {
		sort.Sort(RoutesByPath(routes))
		rc.VirtualHosts = append(rc.VirtualHosts, &VirtualHost{
			Name:    host,
			Domains: []string{host},
			Routes:  routes,
		})
	}

	rcTLS := &HTTPRouteConfig{VirtualHosts: make([]*VirtualHost, 0)}
	for host, routes := range vhostsTLS {
		sort.Sort(RoutesByPath(routes))
		rcTLS.VirtualHosts = append(rcTLS.VirtualHosts, &VirtualHost{
			Name:    host,
			Domains: []string{host},
			Routes:  routes,
		})
	}

	configs := HTTPRouteConfigs{80: rc, 443: rcTLS}
	configs.normalize()
	return configs, tlsAll
}

// buildIngressRoute translates an ingress rule to an Envoy route
func buildIngressRoute(ingress *proxyconfig.IngressRule,
	discovery model.ServiceDiscovery,
	rules []*proxyconfig.RouteRule) ([]*HTTPRoute, string, error) {
	service, exists := discovery.GetService(ingress.Destination)
	if !exists {
		return nil, "", fmt.Errorf("cannot find service %q", ingress.Destination)
	}
	tls := ingress.TlsSecret
	servicePort, err := extractPort(service, ingress)
	if err != nil {
		return nil, "", err
	}

	// unfold the rules for the destination port
	routes := buildDestinationHTTPRoutes(service, servicePort, rules)

	// filter by path, prefix from the ingress
	ingressRoute := buildHTTPRouteMatch(ingress.Match)

	// TODO: not handling header match in ingress apart from uri and authority (uri must not be regex)
	if len(ingressRoute.Headers) > 0 {
		if len(ingressRoute.Headers) > 1 || ingressRoute.Headers[0].Name != model.HeaderAuthority {
			return nil, "", errors.New("header matches in ingress rule not supported")
		}
	}

	out := make([]*HTTPRoute, 0)
	for _, route := range routes {
		if applied := route.CombinePathPrefix(ingressRoute.Path, ingressRoute.Prefix); applied != nil {
			out = append(out, applied)
		}
	}

	return out, tls, nil
}

// extractPort extracts the destination service port from the given destination,
func extractPort(svc *model.Service, ingress *proxyconfig.IngressRule) (*model.Port, error) {
	switch p := ingress.GetDestinationServicePort().(type) {
	case *proxyconfig.IngressRule_DestinationPort:
		num := p.DestinationPort
		port, exists := svc.Ports.GetByPort(int(num))
		if !exists {
			return nil, fmt.Errorf("cannot find port %d in %q", num, svc.Hostname)
		}
		return port, nil
	case *proxyconfig.IngressRule_DestinationPortName:
		name := p.DestinationPortName
		port, exists := svc.Ports.Get(name)
		if !exists {
			return nil, fmt.Errorf("cannot find port %q in %q", name, svc.Hostname)
		}
		return port, nil
	}
	return nil, errors.New("unrecognized destination port")
}
