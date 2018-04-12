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

// Tool to get xDS configs from pilot. It makes an gRPC call to get config, as if it come from envoy
// sidecar, so it will work even with pilot running in local (as long as it use the same kubeconfig)
//
// Usage:
//
// First, you need to expose pilot gRPC port:
//
// * By port-forward existing pilot:
// ```bash
// kubectl port-forward $(k get pod -l istio=pilot -o jsonpath={.items[0].metadata.name} -n istio-system) -n istio-system 15010
// ```
// * Or running local pilot using the same k8s config.
// ```bash
// pilot-discovery discovery --kubeconfig=${HOME}/.kube/config
// ```
//
// Then, to get LDS config, run:
// ```bash
// go run pilot_cli.go -pod <POD_NAME> -ip <POD_IP> -type lds
// ```
// Use cds | eds | rds for other config types.
//
// You can also provide the path to kubeconfig to find pod IP automatically (by default, it will look for the config under
// `${HOME}/.kube/config`)
// ```bash
// go run ./pilot/debug/pilot_cli.go -pod <POD_NAME> -kubeconfig ${HOME}/.kube/config
// ```

package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	ads "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/pkg/log"
	// corev1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	// corev1 "k8s.io/client-go/pkg/api/v1"
	envoy_api_v2_core1 "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/tools/clientcmd"
)

// PodInfo holds information to identify pod.
type PodInfo struct {
	Name      string
	Namespace string
	IP        string
}

func (p *PodInfo) fetchPodIP(kubeconfig string) {
	log.Infof("fetchPodIP for %s.%s with kube config at %s", p.Name, p.Namespace, kubeconfig)
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		panic(err.Error())
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	pods, err := clientset.Core().Pods(p.Namespace).List(meta_v1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	for _, pod := range pods.Items {
		log.Infof("%s %s", pod.Name, pod.Status.HostIP)
		if pod.Name == p.Name {
			p.IP = pod.Status.HostIP
			return
		}
	}
	panic(fmt.Sprintf("Cannot find pod %s.%s in registry.", p.Name, p.Namespace))
}

func (p PodInfo) makeNodeID() string {
	return fmt.Sprintf("sidecar~%s~%s.%s~%s.svc.cluster.local", p.IP, p.Name, p.Namespace, p.Namespace)
}

func makeNodeID(pod, namespace, ip string) string {
	return fmt.Sprintf("sidecar~%s~%s.%s~%s.svc.cluster.local", ip, pod, namespace, namespace)
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

func request(pilotURL string, req *xdsapi.DiscoveryRequest) *xdsapi.DiscoveryResponse {
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
	if kubeConfig == "" {
		return fmt.Sprintf("%s/.kube/config", os.Getenv("HOME"))
	}
	ret, err := filepath.Abs(kubeConfig)
	if err != nil {
		panic(err.Error())
	}
	return ret
}

func main() {
	podName := flag.String("pod", "", "pod name. Require")
	podIP := flag.String("ip", "", "pod IP. Leave blank to use value from registry (need --kubeconfig)")
	namespace := flag.String("namespace", "default", "namespace. Default is 'default'.")
	kubeConfig := flag.String("kubeconfig", "", "path to the kubeconfig file. Leave blank to use the ~/.kube/config")
	pilotURL := flag.String("pilot", "localhost:15010", "pilot address")
	configType := flag.String("type", "lds", "lds, cds, rds or eds. Default lds.")
	outputFile := flag.String("output", "", "output file. Leave blank to go to stdout")
	flag.Parse()

	if *podName == "" {
		panic("--pod cannot be empty")
	}
	if *namespace == "" {
		panic("--namespace cannot be empty")
	}
	if *pilotURL == "" {
		panic("--pilot cannot be empy")
	}

	podInfo := PodInfo{
		Name:      *podName,
		IP:        *podIP,
		Namespace: *namespace,
	}

	if podInfo.IP == "" {
		podInfo.fetchPodIP(resolveKubeConfigPath(*kubeConfig))
	}

	resp := request(*pilotURL, podInfo.makeRequest(*configType))
	strResponse, _ := model.ToJSONWithIndent(resp, " ")
	if outputFile == nil || *outputFile == "" {
		fmt.Printf("%v\n", strResponse)
	} else {
		if err := ioutil.WriteFile(*outputFile, []byte(strResponse), 0644); err != nil {
			panic(err)
		}
	}
}
