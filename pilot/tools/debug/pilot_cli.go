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
// Then, to get LDS config, run:
// ```bash
// go run pilot_cli.go -pod <POD_NAME> -ip <POD_IP> -type lds
// ```
// Use cds | eds | rds for other config types.
//
// For more convience, you can provide the path to kubeconfig to find pod IP automatically,
// and even pod name (using --app flag to find the first pod that has matching app label). The default
// value for kubeconfig path is .kube/config in home folder (works for Linux only).
// ```bash
// go run ./pilot/debug/pilot_cli.go -pod <POD_NAME> -kubeconfig path/to/kube/config
// go run ./pilot/debug/pilot_cli.go -app <APP_LABEL> -kubeconfig path/to/kube/config
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
	AppLabel  string
}

func (p *PodInfo) populatePodNameAndIP(kubeconfig string) bool {
	if p.Name != "" && p.IP != "" {
		// Already set, nothing to do.
		return true
	}
	log.Infof("Using kube config at %s", kubeconfig)
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		log.Errorf(err.Error())
		return false
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Errorf(err.Error())
		return false
	}
	pods, err := clientset.Core().Pods(p.Namespace).List(meta_v1.ListOptions{})
	if err != nil {
		log.Errorf(err.Error())
		return false
	}

	for _, pod := range pods.Items {
		if p.Name != "" && pod.Name == p.Name {
			log.Infof("Found pod %q~%s matching name %q", pod.Name, pod.Status.PodIP, p.Name)
			p.IP = pod.Status.PodIP
			return true
		}
		if app, ok := pod.ObjectMeta.Labels["app"]; ok && app == p.AppLabel {
			log.Infof("Found pod %q~%s matching app label %q", pod.Name, pod.Status.PodIP, p.AppLabel)
			p.Name = pod.Name
			p.IP = pod.Status.PodIP
			return true
		}
	}
	log.Warnf("Cannot find pod matching %v in registry.", p)
	return false
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
	path := strings.Replace(kubeConfig, "~", os.Getenv("HOME"), 1)
	ret, err := filepath.Abs(path)
	if err != nil {
		panic(err.Error())
	}
	return ret
}

func main() {
	//podName := flag.String("pod", "httpbin-v2-68f9d89f7d-cvtts", "pod name. If omit, pod name will be found from k8s registry using app label.")
	podName := flag.String("pod", "httpbin-v1-7cf4bc5f49-zkqw9", "pod name. If omit, pod name will be found from k8s registry using app label.")
	appName := flag.String("app", "", "app label. Should be set if pod name is not provided. It will be used to find "+
		"the pod that has the same app label. Ignored if --pod is set.")
	//podIP := flag.String("ip", "10.8.1.7", "pod IP. If omit, pod IP will be found from registry.")
	podIP := flag.String("ip", "10.8.3.8", "pod IP. If omit, pod IP will be found from registry.")
	namespace := flag.String("namespace", "default", "namespace. Default is 'default'.")
	kubeConfig := flag.String("kubeconfig", "~/.kube/config", "path to the kubeconfig file. Default is ~/.kube/config")
	pilotURL := flag.String("pilot", "localhost:15010", "pilot address")
	configType := flag.String("type", "lds", "lds, cds, rds or eds. Default lds.")
	outputFile := flag.String("output", "", "output file. Leave blank to go to stdout")
	flag.Parse()

	if *podName == "" && *appName == "" {
		log.Error("Either --pod or --app should be set.")
		return
	}
	if *namespace == "" {
		log.Error("--namespace cannot be empty")
		return
	}
	if *pilotURL == "" {
		log.Error("--pilot cannot be empy")
		return
	}

	podInfo := PodInfo{
		Name:      *podName,
		IP:        *podIP,
		Namespace: *namespace,
		AppLabel:  *appName,
	}
	if !podInfo.populatePodNameAndIP(resolveKubeConfigPath(*kubeConfig)) {
		return
	}

	resp := request(*pilotURL, podInfo.makeRequest(*configType))
	strResponse, _ := model.ToJSONWithIndent(resp, " ")
	if outputFile == nil || *outputFile == "" {
		fmt.Printf("%v\n", strResponse)
	} else {
		if err := ioutil.WriteFile(*outputFile, []byte(strResponse), 0644); err != nil {
			log.Errorf("Cannot write output to file %q", *outputFile)
		}
	}
}
