// Copyright 2020 Istio Authors
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

package cmd

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	core1 "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	ads "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	_ "github.com/golang/protobuf/ptypes" //nolint
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp" //nolint
	"k8s.io/client-go/tools/clientcmd"

	v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"

	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

const (
	localPortStart = 50000
	localPortEnd   = 60000
)

// PodInfo holds information to identify pod.
type PodInfo struct {
	Name      string
	Namespace string
	IP        string
	ProxyType string
}

func getAllPods(kubeconfig string) (*v1.PodList, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return clientset.CoreV1().Pods(meta_v1.NamespaceAll).List(meta_v1.ListOptions{})
}

func newPodInfo(nameOrAppLabel string, kubeconfig string, proxyType string) *PodInfo {
	log.Debugf("Using kube config at %s", kubeconfig)
	pods, err := getAllPods(kubeconfig)
	if err != nil {
		log.Errorf(err.Error())
		return nil
	}

	for _, pod := range pods.Items {
		log.Debugf("pod %q", pod.Name)
		if pod.Name == nameOrAppLabel {
			log.Debugf("Found pod %s.%s~%s matching name %q", pod.Name, pod.Namespace, pod.Status.PodIP, nameOrAppLabel)
			return &PodInfo{
				Name:      pod.Name,
				Namespace: pod.Namespace,
				IP:        pod.Status.PodIP,
				ProxyType: proxyType,
			}
		}
		if app, ok := pod.ObjectMeta.Labels["app"]; ok && app == nameOrAppLabel {
			log.Debugf("Found pod %s.%s~%s matching app label %q", pod.Name, pod.Namespace, pod.Status.PodIP, nameOrAppLabel)
			return &PodInfo{
				Name:      pod.Name,
				Namespace: pod.Namespace,
				IP:        pod.Status.PodIP,
				ProxyType: proxyType,
			}
		}
		if istio, ok := pod.ObjectMeta.Labels["istio"]; ok && istio == nameOrAppLabel {
			log.Debugf("Found pod %s.%s~%s matching app label %q", pod.Name, pod.Namespace, pod.Status.PodIP, nameOrAppLabel)
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
	if p.ProxyType != "" {
		return fmt.Sprintf("%s~%s~%s.%s~%s.svc.cluster.local", p.ProxyType, p.IP, p.Name, p.Namespace, p.Namespace)
	}
	if strings.HasPrefix(p.Name, "istio-ingressgateway") || strings.HasPrefix(p.Name, "istio-egressgateway") {
		return fmt.Sprintf("router~%s~%s.%s~%s.svc.cluster.local", p.IP, p.Name, p.Namespace, p.Namespace)
	}
	if strings.HasPrefix(p.Name, "istio-ingress") {
		return fmt.Sprintf("ingress~%s~%s.%s~%s.svc.cluster.local", p.IP, p.Name, p.Namespace, p.Namespace)
	}
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
		Node: &core1.Node{
			Id: p.makeNodeID(),
		},
		TypeUrl: configTypeToTypeURL(configType)}
}

func (p PodInfo) appendResources(req *xdsapi.DiscoveryRequest, resources []string) *xdsapi.DiscoveryRequest {
	req.ResourceNames = resources
	return req
}

var homeVar = env.RegisterStringVar("HOME", "", "")

func resolveKubeConfigPath(kubeConfig string) string {
	path := strings.Replace(kubeConfig, "~", homeVar.Get(), 1)
	ret, err := filepath.Abs(path)
	if err != nil {
		panic(err.Error())
	}
	return ret
}

// nolint: golint
func portForwardPilot(kubeConfig, pilotURL string) (*os.Process, string, error) {
	if pilotURL != "" {
		// No need to port-forward, url is already provided.
		return nil, pilotURL, nil
	}
	log.Debug("Pilot url is not provided, try to port-forward pilot pod.")

	podName := ""
	pods, err := getAllPods(kubeConfig)
	if err != nil {
		return nil, "", err
	}
	for _, pod := range pods.Items {
		if app, ok := pod.ObjectMeta.Labels["istio"]; ok && app == "pilot" {
			podName = pod.Name
		}
	}
	if podName == "" {
		return nil, "", fmt.Errorf("cannot find istio-pilot pod")
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	localPort := r.Intn(localPortEnd-localPortStart) + localPortStart
	cmd := fmt.Sprintf("kubectl port-forward %s -n istio-system %d:15010", podName, localPort)
	parts := strings.Split(cmd, " ")
	c := exec.Command(parts[0], parts[1:]...)
	err = c.Start()
	if err != nil {
		return nil, "", err
	}
	// Make sure istio-pilot is reachable.
	reachable := false
	url := fmt.Sprintf("localhost:%d", localPort)
	for i := 0; i < 10 && !reachable; i++ {
		conn, err := net.Dial("tcp", url)
		if err == nil {
			_ = conn.Close()
			reachable = true
		}
		time.Sleep(1 * time.Second)
	}
	if !reachable {
		return nil, "", fmt.Errorf("cannot reach local pilot url: %s", url)
	}
	return c.Process, fmt.Sprintf("localhost:%d", localPort), nil
}

// PilotClient holds information to make xDS request to pilot.
type PilotClient struct {
	pilotURL           string
	portForwardProcess *os.Process

	streaming bool
}

// NewPilotClient create new pilot client. It will create a port-forward to pilot if needed.
func newPilotClient() *PilotClient {
	process, effectivePilotURL, err := portForwardPilot(resolveKubeConfigPath(kubeConfig), pilotURL)
	if err != nil {
		log.Fatalf("Cannot do port-forwarding for pilot: %v", err)
	}
	return &PilotClient{
		pilotURL:           effectivePilotURL,
		portForwardProcess: process,
		streaming:          streaming,
	}
}

func (c *PilotClient) close() {
	if c.portForwardProcess != nil {
		log.Debugf("Close port-forward process %d", c.portForwardProcess.Pid)
		if err := c.portForwardProcess.Kill(); err != nil {
			log.Errorf("Failed to kill port-forward process, pid: %d", c.portForwardProcess.Pid)
		}
	}
}

type xDSHandler interface {
	makeRequest(pod *PodInfo) *xdsapi.DiscoveryRequest
	onXDSResponse(resp *xdsapi.DiscoveryResponse) error
}

func (c *PilotClient) send(req *xdsapi.DiscoveryRequest, handler xDSHandler) {
	log.Infof("Send xDS request:\n%s\n", req.String())
	conn, err := grpc.Dial(c.pilotURL, grpc.WithInsecure())
	if err != nil {
		panic(err.Error())
	}
	defer func() { _ = conn.Close() }()

	adsClient := ads.NewAggregatedDiscoveryServiceClient(conn)
	stream, err := adsClient.StreamAggregatedResources(context.Background())
	if err != nil {
		log.Fatalf("Cannot call gRPC: %v", err)
	}
	if err := stream.Send(req); err != nil {
		log.Fatalf("Cannot send request: %v", err)
	}
	for {
		log.Infof("Waiting for response .......... ")
		res, err := stream.Recv()
		if err == io.EOF {
			break
		} else if err != nil {
			panic(err.Error())
		}
		log.Infof("Received %s at %s with %d resources", res.TypeUrl, res.VersionInfo, len(res.Resources))
		if err := handler.onXDSResponse(res); err != nil {
			log.Fatalf("Error handle xDS response: %v", err)
		}
		ackReq := &xdsapi.DiscoveryRequest{
			VersionInfo:   res.VersionInfo,
			ResponseNonce: res.Nonce,
			TypeUrl:       req.TypeUrl,
			Node:          req.Node,
			ResourceNames: req.ResourceNames,
		}
		if err := stream.Send(ackReq); err != nil {
			log.Fatalf("Cannot ACK: %v", err)
		}

		if !c.streaming {
			break
		}
	}
}

func outputJSON(p proto.Message) {
	marshaller := jsonpb.Marshaler{
		Indent: "  ",
	}
	var output string
	var err error
	if output, err = marshaller.MarshalToString(p); err != nil {
		log.Fatalf("Cannot convert to JSON: %v", err)
	}

	if len(outputFile) == 0 {
		fmt.Printf("%s\n", output)
	} else if err := ioutil.WriteFile(outputFile, []byte(output), 0644); err != nil {
		log.Errorf("Cannot write output to file %q", outputFile)
	}
}

func makeXDSCmd(use string, handler xDSHandler) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: fmt.Sprintf("Show %s resources", use),
		Long:  fmt.Sprintf("Show %s resources", use),
		Run: func(cmd *cobra.Command, args []string) {
			pilotClient := newPilotClient()
			defer func() {
				pilotClient.close()
			}()

			pod := newPodInfo(proxyTag, resolveKubeConfigPath(kubeConfig), proxyType)
			req := handler.makeRequest(pod)
			pilotClient.send(req, handler)
		},
	}
}
