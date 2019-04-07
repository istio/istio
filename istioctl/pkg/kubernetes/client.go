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

package kubernetes

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"

	multierror "github.com/hashicorp/go-multierror"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/transport/spdy"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/version"
)

var (
	proxyContainer     = "istio-proxy"
	discoveryContainer = "discovery"
	pilotDiscoveryPath = "/usr/local/bin/pilot-discovery"
	pilotAgentPath     = "/usr/local/bin/pilot-agent"
)

// Client is a helper wrapper around the Kube RESTClient for istioctl -> Pilot/Envoy/Mesh related things
type Client struct {
	Config *rest.Config
	*rest.RESTClient
}

// ExecClient is an interface for remote execution
type ExecClient interface {
	EnvoyDo(podName, podNamespace, method, path string, body []byte) ([]byte, error)
	AllPilotsDiscoveryDo(pilotNamespace, method, path string, body []byte) (map[string][]byte, error)
	GetIstioVersions(namespace string) (*version.MeshInfo, error)
	PilotDiscoveryDo(pilotNamespace, method, path string, body []byte) ([]byte, error)
	PodsForSelector(namespace, labelSelector string) (*v1.PodList, error)
	BuildPortForwarder(podName string, ns string, localPort int, podPort int) (*PortForward, error)
}

// PortForward gathers port forwarding results
type PortForward struct {
	Forwarder    *portforward.PortForwarder
	LocalPort    int
	StopChannel  chan struct{}
	ReadyChannel <-chan struct{}
}

// NewClient is the constructor for the client wrapper
func NewClient(kubeconfig, configContext string) (*Client, error) {
	config, err := defaultRestConfig(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}
	restClient, err := rest.RESTClientFor(config)
	if err != nil {
		return nil, err
	}
	return &Client{config, restClient}, nil
}

func defaultRestConfig(kubeconfig, configContext string) (*rest.Config, error) {
	config, err := kube.BuildClientConfig(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}
	config.APIPath = "/api"
	config.GroupVersion = &v1.SchemeGroupVersion
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: scheme.Codecs}
	return config, nil
}

// PodExec takes a command and the pod data to run the command in the specified pod
func (client *Client) PodExec(podName, podNamespace, container string, command []string) (*bytes.Buffer, *bytes.Buffer, error) {
	req := client.Post().
		Resource("pods").
		Name(podName).
		Namespace(podNamespace).
		SubResource("exec").
		Param("container", container).
		VersionedParams(&v1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(client.Config, "POST", req.URL())
	if err != nil {
		return nil, nil, err
	}

	var stdout, stderr bytes.Buffer
	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	})

	return &stdout, &stderr, err
}

// AllPilotsDiscoveryDo makes an http request to each Pilot discovery instance
func (client *Client) AllPilotsDiscoveryDo(pilotNamespace, method, path string, body []byte) (map[string][]byte, error) {
	pilots, err := client.GetIstioPods(pilotNamespace, map[string]string{
		"labelSelector": "istio=pilot",
		"fieldSelector": "status.phase=Running",
	})
	if err != nil {
		return nil, err
	}
	if len(pilots) == 0 {
		return nil, errors.New("unable to find any Pilot instances")
	}
	cmd := []string{"sh", "-c", fmt.Sprintf("GODEBUG= %s request %s %s %s", pilotDiscoveryPath, method, path, string(body))}
	result := map[string][]byte{}
	for _, pilot := range pilots {
		res, err := client.ExtractExecResult(pilot.Name, pilot.Namespace, discoveryContainer, cmd)
		if err != nil {
			return nil, err
		}
		if len(res) > 0 {
			result[pilot.Name] = res
		}
	}
	return result, err
}

// PilotDiscoveryDo makes an http request to a single Pilot discovery instance
func (client *Client) PilotDiscoveryDo(pilotNamespace, method, path string, body []byte) ([]byte, error) {
	pilots, err := client.GetIstioPods(pilotNamespace, map[string]string{
		"labelSelector": "istio=pilot",
		"fieldSelector": "status.phase=Running",
	})
	if err != nil {
		return nil, err
	}
	if len(pilots) == 0 {
		return nil, errors.New("unable to find any Pilot instances")
	}
	cmd := []string{"sh", "-c", fmt.Sprintf("GODEBUG= %s request %s %s %s", pilotDiscoveryPath, method, path, string(body))}
	return client.ExtractExecResult(pilots[0].Name, pilots[0].Namespace, discoveryContainer, cmd)
}

// EnvoyDo makes an http request to the Envoy in the specified pod
func (client *Client) EnvoyDo(podName, podNamespace, method, path string, body []byte) ([]byte, error) {
	container, err := client.GetPilotAgentContainer(podName, podNamespace)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve proxy container name: %v", err)
	}
	cmd := []string{pilotAgentPath, "request", method, path, string(body)}
	return client.ExtractExecResult(podName, podNamespace, container, cmd)
}

// ExtractExecResult wraps PodExec and return the execution result and error if has any.
func (client *Client) ExtractExecResult(podName, podNamespace, container string, cmd []string) ([]byte, error) {
	stdout, stderr, err := client.PodExec(podName, podNamespace, container, cmd)
	if err != nil {
		return nil, fmt.Errorf("error execing into %v/%v %v container: %v", podName, podNamespace, container, err)
	}
	if stderr.String() != "" {
		fmt.Printf("Warning! error execing into %v/%v %v container: %v\n", podName, podNamespace, container, stderr.String())
	}
	return stdout.Bytes(), nil
}

// GetIstioPods retrieves the pod objects for Istio deployments
func (client *Client) GetIstioPods(namespace string, params map[string]string) ([]v1.Pod, error) {
	req := client.Get().
		Resource("pods").
		Namespace(namespace)
	for k, v := range params {
		req.Param(k, v)
	}

	res := req.Do()
	if res.Error() != nil {
		return nil, fmt.Errorf("unable to retrieve Pods: %v", res.Error())
	}
	list := &v1.PodList{}
	if err := res.Into(list); err != nil {
		return nil, fmt.Errorf("unable to parse PodList: %v", res.Error())
	}
	return list.Items, nil
}

// GetPilotAgentContainer retrieves the pilot-agent container name for the specified pod
func (client *Client) GetPilotAgentContainer(podName, podNamespace string) (string, error) {
	req := client.Get().
		Resource("pods").
		Namespace(podNamespace).
		Name(podName)

	res := req.Do()
	if res.Error() != nil {
		return "", fmt.Errorf("unable to retrieve Pod: %v", res.Error())
	}
	pod := &v1.Pod{}
	if err := res.Into(pod); err != nil {
		return "", fmt.Errorf("unable to parse Pod: %v", res.Error())
	}
	for _, c := range pod.Spec.Containers {
		switch c.Name {
		case "egressgateway", "ingress", "ingressgateway":
			return c.Name, nil
		}
	}
	return proxyContainer, nil
}

type podDetail struct {
	binary    string
	container string
}

// GetIstioVersions gets the version for each Istio component
func (client *Client) GetIstioVersions(namespace string) (*version.MeshInfo, error) {
	pods, err := client.GetIstioPods(namespace, map[string]string{
		"labelSelector": "istio",
		"fieldSelector": "status.phase=Running",
	})
	if err != nil {
		return nil, err
	}
	if len(pods) == 0 {
		return nil, errors.New("unable to find any Istio pod in namespace " + namespace)
	}

	labelToPodDetail := map[string]podDetail{
		"pilot":            {"/usr/local/bin/pilot-discovery", "discovery"},
		"citadel":          {"/usr/local/bin/istio_ca", "citadel"},
		"egressgateway":    {"/usr/local/bin/pilot-agent", "istio-proxy"},
		"galley":           {"/usr/local/bin/galley", "galley"},
		"ingressgateway":   {"/usr/local/bin/pilot-agent", "istio-proxy"},
		"telemetry":        {"/usr/local/bin/mixs", "mixer"},
		"policy":           {"/usr/local/bin/mixs", "mixer"},
		"sidecar-injector": {"/usr/local/bin/sidecar-injector", "sidecar-injector-webhook"},
	}

	var errs error
	res := version.MeshInfo{}
	for _, pod := range pods {
		component := pod.Labels["istio"]

		// Special cases
		switch component {
		case "statsd-prom-bridge":
			continue
		case "mixer":
			component = pod.Labels["istio-mixer-type"]
		}

		server := version.ServerInfo{Component: component}

		if detail, ok := labelToPodDetail[component]; ok {
			cmd := []string{detail.binary, "version"}
			cmdJSON := append(cmd, "-o", "json")

			var info version.BuildInfo
			var v version.Version

			stdout, stderr, err := client.PodExec(pod.Name, pod.Namespace, detail.container, cmdJSON)

			if err != nil {
				errs = multierror.Append(errs, fmt.Errorf("error execing into %v %v container: %v", pod.Name, detail.container, err))
				continue
			}

			// At first try parsing stdout
			err = json.Unmarshal(stdout.Bytes(), &v)
			if err == nil && v.ClientVersion.Version != "" {
				info = *v.ClientVersion
			} else {
				// If stdout fails, try the old behavior
				if strings.HasPrefix(stderr.String(), "Error: unknown shorthand flag") {
					stdout, err := client.ExtractExecResult(pod.Name, pod.Namespace, detail.container, cmd)
					if err != nil {
						errs = multierror.Append(errs, fmt.Errorf("error execing into %v %v container: %v", pod.Name, detail.container, err))
						continue
					}

					info, err = version.NewBuildInfoFromOldString(string(stdout))
					if err != nil {
						errs = multierror.Append(errs, fmt.Errorf("error converting server info from JSON: %v", err))
						continue
					}
				} else {
					errs = multierror.Append(errs, fmt.Errorf("error execing into %v %v container: %v", pod.Name, detail.container, stderr.String()))
					continue
				}
			}

			server.Info = info
		}
		res = append(res, server)
	}
	return &res, errs
}

// BuildPortForwarder sets up port forwarding.
//
// nolint: lll
func (client *Client) BuildPortForwarder(podName string, ns string, localPort int, podPort int) (*PortForward, error) {
	var err error
	if localPort == 0 {
		localPort, err = availablePort()
		if err != nil {
			return nil, fmt.Errorf("failure allocating port: %v", err)
		}
	}
	req := client.Post().Resource("pods").Namespace(ns).Name(podName).SubResource("portforward")

	transport, upgrader, err := spdy.RoundTripperFor(client.Config)
	if err != nil {
		return nil, fmt.Errorf("failure creating roundtripper: %v", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())

	stop := make(chan struct{})
	ready := make(chan struct{})
	fw, err := portforward.New(dialer, []string{fmt.Sprintf("%d:%d", localPort, podPort)}, stop, ready, ioutil.Discard, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("failed establishing port-forward: %v", err)
	}

	return &PortForward{
		Forwarder:    fw,
		ReadyChannel: ready,
		StopChannel:  stop,
		LocalPort:    localPort,
	}, nil
}

func availablePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", ":0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	return port, l.Close()
}

func (client *Client) PodsForSelector(namespace, labelSelector string) (*v1.PodList, error) {
	podGet := client.Get().Resource("pods").Namespace(namespace).Param("labelSelector", labelSelector)
	obj, err := podGet.Do().Get()
	if err != nil {
		return nil, fmt.Errorf("failed retrieving pod: %v", err)
	}
	return obj.(*v1.PodList), nil
}

func RunPortForwarder(fw *PortForward, readyFunc func(fw *PortForward) error) error {
	defer fw.Forwarder.Close()

	errCh := make(chan error)
	go func() {
		errCh <- fw.Forwarder.ForwardPorts()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)

	go func() {
		<-signals
		if fw.StopChannel != nil {
			close(fw.StopChannel)
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("failure running port forward process: %v", err)
	case <-fw.ReadyChannel:
		err := readyFunc(fw)
		if err != nil {
			return err
		}

		// wait for interrupt (or connection close)
		<-fw.StopChannel
		return nil
	}
}
