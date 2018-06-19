// Copyright 2018 Istio Authors
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

// Tool to get xDS configs from pilot. This tool simulate envoy sidecar gRPC call to get config,
// so it will work even when sidecar haswhen sidecar hasn't connected (e.g in the case of pilot running on local machine))
//
// Usage:
//
// First, you need to expose pilot gRPC port:
//
// * By port-forward existing pilot:
// ```bash
// kubectl port-forward $(kubectl get pod -l istio=pilot -o jsonpath={.items[0].metadata.name} -n istio-system) -n istio-system 15010
// ```
// * Or run local pilot using the same k8s config.
// ```bash
// pilot-discovery discovery --kubeconfig=${HOME}/.kube/config
// ```
//
// To get LDS or CDS, use -type lds or -type cds, and provide the pod id or app label. For example:
// ```bash
// go run pilot_cli.go -type lds -res httpbin-5766dd474b-2hlnx
// go run pilot_cli.go -type lds -res httpbin
// ```
// Note If more than one pod match with the app label, one will be picked arbitrarily.
//
// For EDS, provide comma-separated-list of clusters. For example:
// ```bash
// go run ./pilot/tools/debug/pilot_cli.go -type eds -res "inbound|http||sleep.default.svc.cluster.local,outbound|http||httpbin.default.svc.cluster.local"
// ```
//
// Script requires kube config in order to connect to k8s registry to get pod information (for LDS and CDS type). The default
// value for kubeconfig path is .kube/config in home folder (works for Linux only). It can be changed via -kubeconfig flag.
// ```bash
// go run ./pilot/debug/pilot_cli.go -type lds -res httpbin -kubeconfig path/to/kube/config
// ```

package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoy_api_v2_core1 "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	ads "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"google.golang.org/grpc"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/pkg/log"
)

// PodInfo holds information to identify pod.
type PodInfo struct {
	Name      string
	Namespace string
	IP        string
}

func NewPodInfo(nameOrAppLabel string, kubeconfig string) *PodInfo {
	log.Infof("Using kube config at %s", kubeconfig)
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		log.Errorf(err.Error())
		return nil
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Errorf(err.Error())
		return nil
	}
	pods, err := clientset.Core().Pods("").List(meta_v1.ListOptions{})
	if err != nil {
		log.Errorf(err.Error())
		return nil
	}

	for _, pod := range pods.Items {
		log.Infof("pod %q", pod.Name)
		if pod.Name == nameOrAppLabel {
			log.Infof("Found pod %s.%s~%s matching name %q", pod.Name, pod.Namespace, pod.Status.PodIP, nameOrAppLabel)
			return &PodInfo{
				Name:      pod.Name,
				Namespace: pod.Namespace,
				IP:        pod.Status.PodIP,
			}
		}
		if app, ok := pod.ObjectMeta.Labels["app"]; ok && app == nameOrAppLabel {
			log.Infof("Found pod %s.%s~%s matching app label %q", pod.Name, pod.Namespace, pod.Status.PodIP, nameOrAppLabel)
			return &PodInfo{
				Name:      pod.Name,
				Namespace: pod.Namespace,
				IP:        pod.Status.PodIP,
			}
		}
	}
	log.Warnf("Cannot find pod with name or app label matching %q in registry.", nameOrAppLabel)
	return nil
}

func (p PodInfo) makeNodeID() string {
	return fmt.Sprintf("sidecar~%s~%s.%s~%s.svc.cluster.local", p.IP, p.Name, p.Namespace, p.Namespace)
}

func configTypeToTypeURL(configType string) string {
	switch configType {
	case "lds":
		return v2.ListenerType
	case "cds":
		return v2.ClusterType
	case "rds":
		return v2.RouteType
	case "eds":
		return v2.EndpointType
	default:
		panic(fmt.Sprintf("Unknown type %s", configType))
	}
}

func (p PodInfo) makeRequest(configType string) *xdsapi.DiscoveryRequest {
	return &xdsapi.DiscoveryRequest{
		Node: &envoy_api_v2_core1.Node{
			Id: p.makeNodeID(),
		},
		TypeUrl: configTypeToTypeURL(configType)}
}

func (p PodInfo) getResource(pilotURL string, configType string) *xdsapi.DiscoveryResponse {
	conn, err := grpc.Dial(pilotURL, grpc.WithInsecure())
	if err != nil {
		panic(err.Error())
	}
	defer conn.Close()

	adsClient := ads.NewAggregatedDiscoveryServiceClient(conn)
	stream, err := adsClient.StreamAggregatedResources(context.Background())
	if err != nil {
		panic(err.Error())
	}
	err = stream.Send(p.makeRequest(configType))
	if err != nil {
		panic(err.Error())
	}
	res, err := stream.Recv()
	if err != nil {
		panic(err.Error())
	}
	return res
}

func makeEDSRequest(resources string) *xdsapi.DiscoveryRequest {
	return &xdsapi.DiscoveryRequest{
		ResourceNames: strings.Split(resources, ","),
	}
}

func edsRequest(pilotURL string, req *xdsapi.DiscoveryRequest) *xdsapi.DiscoveryResponse {
	conn, err := grpc.Dial(pilotURL, grpc.WithInsecure())
	if err != nil {
		panic(err.Error())
	}
	defer conn.Close()

	adsClient := xdsapi.NewEndpointDiscoveryServiceClient(conn)
	stream, err := adsClient.StreamEndpoints(context.Background())
	if err != nil {
		panic(err.Error())
	}
	err = stream.Send(req)
	if err != nil {
		panic(err.Error())
	}
	res, err := stream.Recv()
	if err != nil {
		panic(err.Error())
	}
	return res
}

func resolveKubeConfigPath(kubeConfig string) string {
	path := strings.Replace(kubeConfig, "~", os.Getenv("HOME"), 1)
	ret, err := filepath.Abs(path)
	if err != nil {
		panic(err.Error())
	}
	return ret
}

func main() {
	kubeConfig := flag.String("kubeconfig", "~/.kube/config", "path to the kubeconfig file. Default is ~/.kube/config")
	pilotURL := flag.String("pilot", "localhost:15010", "pilot address")
	configType := flag.String("type", "lds", "lds, cds, or eds. Default lds.")
	resources := flag.String("res", "", "Resource(s) to get config for. Should be pod name or app label for lds and cds type. For eds, it is comma separated list of cluster name.")
	outputFile := flag.String("out", "", "output file. Leave blank to go to stdout")
	flag.Parse()

	var resp *xdsapi.DiscoveryResponse
	if *configType == "lds" || *configType == "cds" {
		pod := NewPodInfo(*resources, resolveKubeConfigPath(*kubeConfig))
		resp = pod.getResource(*pilotURL, *configType)
	} else if *configType == "eds" {
		resp = edsRequest(*pilotURL, makeEDSRequest(*resources))
	} else {
		log.Errorf("Unknown config type: %q", *configType)
		os.Exit(1)
	}

	strResponse, _ := model.ToJSONWithIndent(resp, " ")
	if outputFile == nil || *outputFile == "" {
		fmt.Printf("%v\n", strResponse)
	} else {
		if err := ioutil.WriteFile(*outputFile, []byte(strResponse), 0644); err != nil {
			log.Errorf("Cannot write output to file %q", *outputFile)
		}
	}
}
