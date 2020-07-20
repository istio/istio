package xds

import (
	"bytes"
	"testing"
	"text/template"

	"github.com/Masterminds/sprig"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/test"
)

var configgen = core.NewConfigGenerator([]string{plugin.Authn, plugin.Authz, plugin.Health, plugin.Mixer})

type SidecarTestConfig struct {
	ImportedNamespaces []string
	Resolution         string
	IngressListener    bool
}

var scopeConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: sidecar
  namespace:  app
spec:
{{- if .IngressListener }}
  ingress:
    - port:
        number: 9080
        protocol: HTTP
        name: custom-http
      defaultEndpoint: unix:///var/run/someuds.sock
{{- end }}
  egress:
    - hosts:
{{ range $i, $ns := .ImportedNamespaces }}
      - {{$ns}}
{{ end }}
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: app
  namespace: app
spec:
  hosts:
  - app.com
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: {{.Resolution}}
  endpoints:
{{- if eq .Resolution "DNS" }}
  - address: app.com
{{- else }}
  - address: 1.1.1.1
{{- end }}
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: excluded
  namespace: excluded
spec:
  hosts:
  - app.com
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: {{.Resolution}}
  endpoints:
{{- if eq .Resolution "DNS" }}
  - address: excluded.com
{{- else }}
  - address: 9.9.9.9
{{- end }}
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: included
  namespace: included
spec:
  hosts:
  - app.com
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: {{.Resolution}}
  endpoints:
{{- if eq .Resolution "DNS" }}
  - address: included.com
{{- else }}
  - address: 2.2.2.2
{{- end }}
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: app-https
  namespace: app
spec:
  hosts:
  - app.cluster.local
  addresses:
  - 5.5.5.5
  ports:
  - number: 443
    name: https
    protocol: HTTPS
  resolution: {{.Resolution}}
  endpoints:
{{- if eq .Resolution "DNS" }}
  - address: app.com
{{- else }}
  - address: 10.10.10.10
{{- end }}
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: excluded-https
  namespace: excluded
spec:
  hosts:
  - app.cluster.local
  addresses:
  - 5.5.5.5
  ports:
  - number: 4431
    name: https
    protocol: HTTPS
  resolution: {{.Resolution}}
  endpoints:
{{- if eq .Resolution "DNS" }}
  - address: app.com
{{- else }}
  - address: 10.10.10.10
{{- end }}
`

func templateToConfig(t test.Failer, tmplString string, input interface{}) []model.Config {
	t.Helper()
	tmpl := template.Must(template.New("").Funcs(sprig.TxtFuncMap()).Parse(tmplString))
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, input); err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}
	configs, badKinds, err := crd.ParseInputs(buf.String())
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	if len(badKinds) != 0 {
		t.Fatalf("Got unknown resources: %v", badKinds)
	}
	// setup default namespace if not defined
	for i, c := range configs {
		if c.Namespace == "" {
			c.Namespace = "default"
		}
		configs[i] = c
	}
	return configs
}

func setupFakeDiscovery(t test.Failer, config string, input interface{}) (*DiscoveryServer, *model.Environment) {
	cfgs := templateToConfig(t, config, input)

	stop := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
	})

	configStore := memory.MakeWithLedger(collections.Pilot, &model.DisabledLedger{}, true)
	env := &model.Environment{}
	s := NewDiscoveryServer(env, []string{})
	s.ConfigGenerator = configgen

	configController := memory.NewSyncController(configStore)
	go configController.Run(stop)
	serviceDiscovery := serviceentry.NewServiceDiscovery(configController, model.MakeIstioStore(configStore), s)
	for _, cfg := range cfgs {
		if _, err := configController.Create(cfg); err != nil {
			t.Fatalf("failed to create config %v: %v", cfg.Name, err)
		}
	}


	m := mesh.DefaultMeshConfig()

	env.PushContext = model.NewPushContext()
	env.ServiceDiscovery = serviceDiscovery
	env.IstioConfigStore = model.MakeIstioStore(configStore)
	env.Watcher = mesh.NewFixedWatcher(&m)

	if err := s.UpdateServiceShards(env.PushContext); err != nil {
		t.Fatal(err)
	}
	if err := env.PushContext.InitContext(env, nil, nil); err != nil {
		t.Fatal(err)
	}
	return s, env
}

func setupProxy(proxy *model.Proxy, env *model.Environment) *model.Proxy {
	proxy.SetSidecarScope(env.PushContext)
	proxy.SetGatewaysForProxy(env.PushContext)
	proxy.SetServiceInstances(env.ServiceDiscovery)
	proxy.DiscoverIPVersions()
	return proxy
}

func TestServiceEntryDNS(t *testing.T) {
	s, env := setupFakeDiscovery(t, scopeConfig, SidecarTestConfig{
		ImportedNamespaces: []string{"./*"},
		Resolution:         "STATIC",
		IngressListener:    false,
	})
	proxy := setupProxy(&model.Proxy{
		Metadata:        &model.NodeMetadata{},
		ID:              "app.app",
		Type:            model.SidecarProxy,
		IPAddresses:     []string{"1.1.1.1"},
		ConfigNamespace: "app",
	}, env)

	clusters := configgen.BuildClusters(proxy, env.PushContext)
	endpoints := extractEndpoints(buildEndpoints(extractEdsClusterNames(clusters), proxy, env.PushContext, s))
	if !listEqualUnordered(endpoints["outbound|80||app.com"], []string{"1.1.1.1"}) {
		t.Fatalf("expected 1.1.1.1, got %v", endpoints["outbound|80||app.com"])
	}

	assertListeners(t, []string{
		"0.0.0.0_80",
		"5.5.5.5_443",
		"virtualInbound",
		"virtualOutbound",
	}, configgen.BuildListeners(proxy, env.PushContext))
}

func buildEndpoints(clusters []string, proxy *model.Proxy, push *model.PushContext, s *DiscoveryServer) []*endpoint.ClusterLoadAssignment {
	loadAssignments := make([]*endpoint.ClusterLoadAssignment, 0)
	for _, c := range clusters {
		l := s.generateEndpoints(c, proxy, push, nil)
		loadAssignments = append(loadAssignments, l)
	}
	return loadAssignments
}

func extractEndpoints(endpoints []*endpoint.ClusterLoadAssignment) map[string][]string {
	got := map[string][]string{}
	for _, cla := range endpoints {
		for _, ep := range cla.Endpoints {
			for _, lb := range ep.LbEndpoints {
				got[cla.ClusterName] = append(got[cla.ClusterName], lb.GetEndpoint().Address.GetSocketAddress().Address)
			}
		}
	}
	return got
}

func assertListeners(t test.Failer, expected []string, listeners []*listener.Listener) {
	t.Helper()
	got := extractListenerNames(listeners)
	if !listEqualUnordered(got, expected) {
		t.Fatalf("expected listeners %v, got %v", expected, listeners)
	}
}

func extractListenerNames(ll []*listener.Listener) []string {
	res := []string{}
	for _, l := range ll {
		res = append(res, l.Name)
	}
	return res
}

func extractEdsClusterNames(cl []*cluster.Cluster) []string {
	res := []string{}
	for _, c := range cl {
		switch v := c.ClusterDiscoveryType.(type) {
		case *cluster.Cluster_Type:
			if v.Type != cluster.Cluster_EDS {
				continue
			}
		}
		res = append(res, c.Name)
	}
	return res
}
