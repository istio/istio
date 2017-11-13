package v2

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"time"

	"github.com/envoyproxy/go-control-plane/api"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	"github.com/golang/glog"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	jsonnet "github.com/google/go-jsonnet"
	multierror "github.com/hashicorp/go-multierror"
	"istio.io/istio/pilot/adapter/config/crd"
	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/platform/kube"
)

type Generator struct {
	services *kube.Controller
	config   model.ConfigStoreCache
	vm       *jsonnet.VM
	script   string
	version  int

	// Cache needs to be set
	Cache cache.Cache
	Path  string
}

func NewGenerator(out cache.Cache, kubeconfig string, watchedNamespace string) (*Generator, error) {
	_, client, kuberr := kube.CreateInterface(kubeconfig)
	if kuberr != nil {
		return nil, multierror.Prefix(kuberr, "failed to connect to Kubernetes API.")
	}

	options := kube.ControllerOptions{
		WatchedNamespace: watchedNamespace,
		ResyncPeriod:     60 * time.Second,
		DomainSuffix:     "cluster.local",
	}

	configClient, err := crd.NewClient(kubeconfig, model.ConfigDescriptor{
		model.RouteRule,
		model.EgressRule,
		model.DestinationPolicy,
	}, options.DomainSuffix)

	if err != nil {
		return nil, multierror.Prefix(err, "failed to open a config client.")
	}

	if err = configClient.RegisterResources(); err != nil {
		return nil, multierror.Prefix(err, "failed to register custom resources.")
	}

	configController := crd.NewController(configClient, options)
	ctl := kube.NewController(client, options)
	g := &Generator{
		services: ctl,
		config:   configController,
		Cache:    out,
		Path:     ".",
	}

	// register handlers
	if err := ctl.AppendServiceHandler(g.UpdateServices); err != nil {
		return nil, err
	}
	if err := ctl.AppendInstanceHandler(g.UpdateInstances); err != nil {
		return nil, err
	}

	configController.RegisterEventHandler(model.RouteRule.Type, g.UpdateConfig)
	configController.RegisterEventHandler(model.IngressRule.Type, g.UpdateConfig)
	configController.RegisterEventHandler(model.EgressRule.Type, g.UpdateConfig)
	configController.RegisterEventHandler(model.DestinationPolicy.Type, g.UpdateConfig)
	return g, nil
}

func (g *Generator) Run(stop <-chan struct{}) {
	go g.services.Run(stop)
	go g.config.Run(stop)
}

type stuff struct {
	Listeners []interface{} `json:"listeners"`
	Routes    []interface{} `json:"routes"`
	Clusters  []interface{} `json:"clusters"`
}

func (g *Generator) PrepareProgram() {
	if g.vm == nil {
		glog.Infof("prepare jsonnet VM")
		vm := jsonnet.MakeVM()
		vm.Importer(&jsonnet.FileImporter{JPaths: []string{g.Path}})
		content, err := ioutil.ReadFile("envoy.jsonnet")
		if err != nil {
			glog.Fatal(err)
		}
		g.vm = vm
		g.script = string(content)
	}
}

func (g *Generator) Generate() {
	g.PrepareProgram()
	glog.Infof("generating snapshot %d", g.version)
	in, err := g.vm.EvaluateSnippet("envoy.jsonnet", g.script)
	if err != nil {
		glog.Warning(err)
	}

	out := stuff{}
	if err := json.Unmarshal([]byte(in), &out); err != nil {
		glog.Warning(err)
	}

	listeners := make([]proto.Message, 0)
	for _, listener := range out.Listeners {
		l := api.Listener{}
		s, _ := json.Marshal(listener)
		listeners = append(listeners, &l)
		if err := jsonpb.UnmarshalString(string(s), &l); err != nil {
			log.Fatal(err)
		}
	}

	clusters := make([]proto.Message, 0)
	for _, cluster := range out.Clusters {
		l := api.Cluster{}
		s, _ := json.Marshal(cluster)
		clusters = append(clusters, &l)
		if err := jsonpb.UnmarshalString(string(s), &l); err != nil {
			log.Fatal(err)
		}
	}

	routes := make([]proto.Message, 0)
	for _, route := range out.Routes {
		r := api.RouteConfiguration{}
		s, _ := json.Marshal(route)
		routes = append(routes, &r)
		if err := jsonpb.UnmarshalString(string(s), &r); err != nil {
			log.Fatal(err)
		}
	}

	g.version++
	snapshot := cache.NewSnapshot(fmt.Sprintf("%d", g.version),
		nil, /* TODO */
		clusters,
		routes,
		listeners)
	if g.Cache != nil {
		g.Cache.SetSnapshot("service-node", snapshot)
	}
}

func (g *Generator) UpdateServices(*model.Service, model.Event) {
	out, err := g.services.Services()
	if err != nil {
		glog.Warning(err)
		return
	}

	bytes, err := json.Marshal(out)
	if err != nil {
		glog.Warning(err)
		return
	}

	err = ioutil.WriteFile("services.json", bytes, 0600)
	if err != nil {
		glog.Warning(err)
	}

	g.Generate()
}

func (g *Generator) UpdateInstances(*model.ServiceInstance, model.Event) {
	out, err := g.services.HostInstances(nil)
	if err != nil {
		glog.Warning(err)
		return
	}

	bytes, err := json.Marshal(out)
	if err != nil {
		glog.Warning(err)
		return
	}

	err = ioutil.WriteFile("instances.json", bytes, 0600)
	if err != nil {
		glog.Warning(err)
	}

	g.Generate()
}

func (g *Generator) UpdateConfig(model.Config, model.Event) {
	// g.Generate()
}
