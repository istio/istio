package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"time"

	"github.com/envoyproxy/go-control-plane/api"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	xds "github.com/envoyproxy/go-control-plane/pkg/grpc"
	"github.com/envoyproxy/go-control-plane/pkg/test"
	"github.com/golang/glog"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	jsonnet "github.com/google/go-jsonnet"
	multierror "github.com/hashicorp/go-multierror"
	"google.golang.org/grpc"
	"istio.io/istio/pilot/adapter/config/crd"
	"istio.io/istio/pilot/cmd"
	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/platform/kube"
)

type stuff struct {
	Bootstrap interface{}   `json:"bootstrap"`
	Listeners []interface{} `json:"listeners"`
	Routes    []interface{} `json:"routes"`
	Clusters  []interface{} `json:"clusters"`
}

type importer struct {
}

func main() {
	flag.Parse()
	vm := jsonnet.MakeVM()
	vm.Importer(&jsonnet.FileImporter{JPaths: []string{"."}})
	content, err := ioutil.ReadFile("envoy.jsonnet")
	if err != nil {
		log.Fatal(err)
	}
	in, err := vm.EvaluateSnippet("envoy.jsonnet", string(content))
	if err != nil {
		log.Fatal(err)
	}

	out := stuff{}
	if err := json.Unmarshal([]byte(in), &out); err != nil {
		log.Fatal(err)
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
	r2 := api.Bootstrap{}
	s2, _ := json.Marshal(out.Bootstrap)
	if err := jsonpb.UnmarshalString(string(s2), &r2); err != nil {
		log.Fatal(err)
	}
	log.Printf("%v", clusters)

	config := cache.NewSimpleCache(test.Hasher{}, nil)
	snapshot := cache.NewSnapshot("test",
		[]proto.Message{},
		clusters,
		routes,
		listeners)
	config.SetSnapshot("service-node", snapshot)
	server := xds.NewServer(config)
	grpcServer := grpc.NewServer()
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", 15003))
	if err != nil {
		glog.Fatalf("failed to listen: %v", err)
	}
	server.Register(grpcServer)
	if err = grpcServer.Serve(lis); err != nil {
		glog.Error(err)
	}
}

type args struct {
	kubeconfig string
	meshconfig string

	// namespace for the controller (typically istio installation namespace)
	namespace string

	// ingress sync mode is set to off by default
	controllerOptions kube.ControllerOptions
}

var (
	flags args
)

func run() error {
	stop := make(chan struct{})

	if flags.namespace == "" {
		flags.namespace = os.Getenv("POD_NAMESPACE")
	}

	_, client, kuberr := kube.CreateInterface(flags.kubeconfig)
	if kuberr != nil {
		return multierror.Prefix(kuberr, "failed to connect to Kubernetes API.")
	}

	configClient, err := crd.NewClient(flags.kubeconfig, model.ConfigDescriptor{
		model.RouteRule,
		model.EgressRule,
		model.DestinationPolicy,
	}, flags.controllerOptions.DomainSuffix)
	if err != nil {
		return multierror.Prefix(err, "failed to open a config client.")
	}

	if err = configClient.RegisterResources(); err != nil {
		return multierror.Prefix(err, "failed to register custom resources.")
	}

	configController := crd.NewController(configClient, flags.controllerOptions)
	kubectl := kube.NewController(client, flags.controllerOptions)

	go kubectl.Run(stop)
	go configController.Run(stop)
	cmd.WaitSignal(stop)
	return nil
}

func init() {
	flag.StringVar(&flags.kubeconfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	flag.StringVar(&flags.meshconfig, "meshConfig", "/etc/istio/config/mesh",
		fmt.Sprintf("File name for Istio mesh configuration"))
	flag.StringVar(&flags.namespace, "namespace", "",
		"Select a namespace where the controller resides. If not set, uses ${POD_NAMESPACE} environment variable")
	flag.StringVar(&flags.controllerOptions.WatchedNamespace, "appNamespace", "",
		"Restrict the applications namespace the controller manages; if not set, controller watches all namespaces")
	flag.DurationVar(&flags.controllerOptions.ResyncPeriod, "resync", 60*time.Second,
		"Controller resync interval")
	flag.StringVar(&flags.controllerOptions.DomainSuffix, "domain", "cluster.local",
		"DNS domain suffix")
}
