package v2

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"path"
	"sort"
	"time"

	"github.com/envoyproxy/go-control-plane/api"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	"github.com/golang/glog"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	jsonnet "github.com/google/go-jsonnet"
	multierror "github.com/hashicorp/go-multierror"
	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/platform/kube"
)

type Hasher struct{}

func (Hasher) Hash(node *api.Node) (cache.Key, error) {
	return "", nil
}

type Generator struct {
	services      *kube.Controller
	servicesHash  [md5.Size]byte
	instancesHash [md5.Size]byte

	vm     *jsonnet.VM
	script string

	version   int
	endpoints []proto.Message
	clusters  []proto.Message
	routes    []proto.Message
	listeners []proto.Message

	// Cache needs to be set
	Cache cache.Cache
	ID    string
	Path  string
}

func NewGenerator(out cache.Cache, jpath, id, domain, kubeconfig string) (*Generator, error) {
	_, client, kuberr := kube.CreateInterface(kubeconfig)
	if kuberr != nil {
		return nil, multierror.Prefix(kuberr, "failed to connect to Kubernetes API.")
	}

	options := kube.ControllerOptions{
		ResyncPeriod: 60 * time.Second,
		DomainSuffix: "cluster.local",
	}

	bytes, err := json.Marshal(context{Domain: domain})
	if err != nil {
		return nil, err
	}
	err = ioutil.WriteFile(path.Join(jpath, "context.json"), bytes, 0600)
	if err != nil {
		return nil, err
	}

	ctl := kube.NewController(client, options)
	g := &Generator{
		services: ctl,
		Cache:    out,
		ID:       id,
		Path:     jpath,
	}

	// register handlers
	if err := ctl.AppendServiceHandler(g.UpdateServices); err != nil {
		return nil, err
	}
	if err := ctl.AppendInstanceHandler(g.UpdateInstances); err != nil {
		return nil, err
	}

	return g, nil
}

func (g *Generator) Run(stop <-chan struct{}) {
	go g.services.Run(stop)
}

type context struct {
	Domain string `json:"domain"`
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
	updated := false

	if g.clusters == nil || g.routes == nil || g.listeners == nil {
		glog.Infof("generating snapshot %d", g.version)
		in, err := g.vm.EvaluateSnippet("envoy.jsonnet", g.script)
		if err != nil {
			glog.Warning(err)
		}

		out := stuff{}
		if err := json.Unmarshal([]byte(in), &out); err != nil {
			glog.Warning(err)
		}

		g.clusters = make([]proto.Message, 0)
		for _, cluster := range out.Clusters {
			l := api.Cluster{}
			s, _ := json.Marshal(cluster)
			if err := jsonpb.UnmarshalString(string(s), &l); err != nil {
				log.Fatal(err)
			}
			g.clusters = append(g.clusters, &l)
		}

		g.routes = make([]proto.Message, 0)
		for _, route := range out.Routes {
			r := api.RouteConfiguration{}
			s, _ := json.Marshal(route)
			if err := jsonpb.UnmarshalString(string(s), &r); err != nil {
				log.Fatal(err)
			}
			g.routes = append(g.routes, &r)
		}

		g.listeners = make([]proto.Message, 0)
		for _, listener := range out.Listeners {
			l := api.Listener{}
			s, _ := json.Marshal(listener)
			if err := jsonpb.UnmarshalString(string(s), &l); err != nil {
				log.Fatal(err)
			}
			g.listeners = append(g.listeners, &l)
		}

		updated = true
	}

	// populate endpoints from clusters
	if g.services != nil {
		endpoints := make([]proto.Message, 0, len(g.clusters))
		for _, msg := range g.clusters {
			cluster := msg.(*api.Cluster)
			// note that EDS present service name instead of cluster name here
			if cluster.EdsClusterConfig != nil {
				hostname, ports, labelcols := model.ParseServiceKey(cluster.EdsClusterConfig.ServiceName)
				instances, err := g.services.Instances(hostname, ports.GetNames(), labelcols)
				if err != nil {
					glog.Warning(err)
				}
				addresses := make([]*api.LbEndpoint, 0, len(instances))
				for _, instance := range instances {
					addresses = append(addresses, &api.LbEndpoint{
						Endpoint: &api.Endpoint{
							Address: &api.Address{
								Address: &api.Address_SocketAddress{
									SocketAddress: &api.SocketAddress{
										Address:       instance.Endpoint.Address,
										PortSpecifier: &api.SocketAddress_PortValue{PortValue: uint32(instance.Endpoint.Port)},
									},
								},
							},
						},
					})
				}
				endpoints = append(endpoints, &api.ClusterLoadAssignment{
					ClusterName: cluster.EdsClusterConfig.ServiceName,
					Endpoints:   []*api.LocalityLbEndpoints{{LbEndpoints: addresses}}})
			}
		}
		g.endpoints = endpoints
		updated = true
		// TODO: avoid churn by sorting and caching
	}

	if updated && g.Cache != nil {
		g.version++
		snapshot := cache.NewSnapshot(fmt.Sprintf("%d", g.version),
			g.endpoints,
			g.clusters,
			g.routes,
			g.listeners)
		g.Cache.SetSnapshot("", snapshot)
	}
}

func (g *Generator) UpdateServices(*model.Service, model.Event) {
	out, err := g.services.Services()
	if err != nil {
		glog.Warning(err)
		return
	}
	if out == nil {
		out = []*model.Service{}
	}

	// sort by hostnames
	sort.Slice(out, func(i, j int) bool { return out[i].Hostname < out[j].Hostname })

	bytes, err := json.Marshal(out)
	if err != nil {
		glog.Warning(err)
		return
	}

	if hash := md5.Sum(bytes); hash != g.servicesHash {
		g.routes = nil
		g.servicesHash = hash
	}

	err = ioutil.WriteFile(path.Join(g.Path, "services.json"), bytes, 0600)
	if err != nil {
		glog.Warning(err)
	}

	g.Generate()
}

func (g *Generator) UpdateInstances(*model.ServiceInstance, model.Event) {
	out, err := g.services.WorkloadInstances(g.ID)
	if err != nil {
		glog.Warning(err)
		return
	}

	if out == nil {
		out = []model.ServiceInstance{}
	}

	// sort by hostname/ip/port
	sort.Slice(out, func(i, j int) bool {
		return out[i].Service.Hostname < out[j].Service.Hostname ||
			(out[i].Service.Hostname == out[j].Service.Hostname &&
				out[i].Endpoint.Port < out[j].Endpoint.Port)
	})

	bytes, err := json.Marshal(out)
	if err != nil {
		glog.Warning(err)
		return
	}

	if hash := md5.Sum(bytes); hash != g.instancesHash {
		g.routes = nil
		g.instancesHash = hash
	}

	err = ioutil.WriteFile(path.Join(g.Path, "instances.json"), bytes, 0600)
	if err != nil {
		glog.Warning(err)
	}

	g.Generate()
}
