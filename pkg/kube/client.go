// Copyright Istio Authors
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

package kube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	"github.com/hashicorp/go-multierror"
	v1 "k8s.io/api/core/v1"
	kubeExtClient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	kubeVersion "k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	"istio.io/api/label"

	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

const (
	defaultLocalAddress = "localhost"
)

// Client is a helper for common Kubernetes client operations
type Client interface {
	kubernetes.Interface
	rest.Interface

	// Config returns the Kubernetes rest config used to configure the clients.
	Config() *rest.Config

	// Ext returns the API extensions client.
	Ext() kubeExtClient.Interface

	// Revision of the Istio control plane.
	Revision() string

	// GetKubernetesVersion returns the Kubernetes server version
	GetKubernetesVersion() (*kubeVersion.Info, error)

	// EnvoyDo makes an http request to the Envoy in the specified pod.
	EnvoyDo(ctx context.Context, podName, podNamespace, method, path string, body []byte) ([]byte, error)

	// AllDiscoveryDo makes an http request to each Istio discovery instance.
	AllDiscoveryDo(ctx context.Context, namespace, path string) (map[string][]byte, error)

	// GetIstioVersions gets the version for each Istio control plane component.
	GetIstioVersions(ctx context.Context, namespace string) (*version.MeshInfo, error)

	// PodsForSelector finds pods matching selector.
	PodsForSelector(ctx context.Context, namespace, labelSelector string) (*v1.PodList, error)

	// GetIstioPods retrieves the pod objects for Istio deployments
	GetIstioPods(ctx context.Context, namespace string, params map[string]string) ([]v1.Pod, error)

	// PodExec takes a command and the pod data to run the command in the specified pod.
	PodExec(podName, podNamespace, container string, command []string) (*bytes.Buffer, *bytes.Buffer, error)

	// PodLogs retrieves the logs for the given pod.
	PodLogs(ctx context.Context, podName string, podNamespace string, container string, previousLog bool) (string, error)

	// NewPortForwarder creates a new PortForwarder configured for the given pod. If localPort=0, a port will be
	// dynamically selected. If localAddress is empty, "localhost" is used.
	NewPortForwarder(podName string, ns string, localAddress string, localPort int, podPort int) (PortForwarder, error)
}

var _ Client = &client{}

// Client is a helper wrapper around the Kube RESTClient for istioctl -> Pilot/Envoy/Mesh related things
type client struct {
	*kubernetes.Clientset
	*rest.RESTClient
	config   *rest.Config
	extSet   *kubeExtClient.Clientset
	revision string
}

// NewClientForKubeConfig creates a client wrapper for the given kubeconfig file.
func NewClientForKubeConfig(kubeconfig, context string) (Client, error) {
	config, err := DefaultRestConfig(kubeconfig, context)
	if err != nil {
		return nil, err
	}
	return NewClient(config)
}

// NewClient is the constructor for the client wrapper
func NewClient(config *rest.Config) (Client, error) {
	return NewClientWithRevision(config, "")
}

// NewClientWithRevision is a constructor for the client wrapper that supports dual/multiple control plans
func NewClientWithRevision(config *rest.Config, revision string) (Client, error) {
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	restClient, err := rest.RESTClientFor(config)
	if err != nil {
		return nil, err
	}
	extSet, err := kubeExtClient.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return &client{
		Clientset:  clientSet,
		RESTClient: restClient,
		config:     config,
		extSet:     extSet,
		revision:   revision}, nil
}

func (c *client) Config() *rest.Config {
	return c.config
}

func (c *client) Ext() kubeExtClient.Interface {
	return c.extSet
}

func (c *client) Revision() string {
	return c.revision
}

func (c *client) GetKubernetesVersion() (*kubeVersion.Info, error) {
	return c.extSet.ServerVersion()
}

func (c *client) PodExec(podName, podNamespace, container string, command []string) (*bytes.Buffer, *bytes.Buffer, error) {
	req := c.Post().
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

	wrapper, upgrader, err := roundTripperFor(c.config)
	if err != nil {
		return nil, nil, err
	}
	exec, err := remotecommand.NewSPDYExecutorForTransports(wrapper, upgrader, "POST", req.URL())
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

func (c *client) PodLogs(ctx context.Context, namespace string, pod string, container string, previousLog bool) (string, error) {
	opts := &v1.PodLogOptions{
		Container: container,
		Previous:  previousLog,
	}
	res, err := c.CoreV1().Pods(namespace).GetLogs(pod, opts).Stream(ctx)
	if err != nil {
		return "", err
	}
	defer closeQuietly(res)

	builder := &strings.Builder{}
	if _, err = io.Copy(builder, res); err != nil {
		return "", err
	}

	return builder.String(), nil
}

// ProxyGet returns a response of the pod by calling it through the proxy.
// Not a part of client-go https://github.com/kubernetes/kubernetes/issues/90768
func (c *client) proxyGet(name, namespace, path string, port int) rest.ResponseWrapper {
	pathURL, err := url.Parse(path)
	if err != nil {
		log.Errorf("failed to parse path %s: %v", path, err)
		pathURL = &url.URL{Path: path}
	}
	request := c.RESTClient.Get().
		Namespace(namespace).
		Resource("pods").
		SubResource("proxy").
		Name(fmt.Sprintf("%s:%d", name, port)).
		Suffix(pathURL.Path)
	for key, vals := range pathURL.Query() {
		for _, val := range vals {
			request = request.Param(key, val)
		}
	}
	return request
}

func (c *client) AllDiscoveryDo(ctx context.Context, pilotNamespace, path string) (map[string][]byte, error) {
	pilots, err := c.GetIstioPods(ctx, pilotNamespace, map[string]string{
		"labelSelector": "app=istiod",
		"fieldSelector": "status.phase=Running",
	})
	if err != nil {
		return nil, err
	}
	if len(pilots) == 0 {
		return nil, errors.New("unable to find any Pilot instances")
	}
	result := map[string][]byte{}
	for _, pilot := range pilots {
		res, err := c.proxyGet(pilot.Name, pilot.Namespace, path, 8080).DoRaw(ctx)
		if err != nil {
			return nil, err
		}
		if len(res) > 0 {
			result[pilot.Name] = res
		}
	}
	return result, err
}

func (c *client) EnvoyDo(ctx context.Context, podName, podNamespace, method, path string, _ []byte) ([]byte, error) {
	formatError := func(err error) error {
		return fmt.Errorf("failure running port forward process: %v", err)
	}

	fw, err := c.NewPortForwarder(podName, podNamespace, "127.0.0.1", 0, 15000)
	if err != nil {
		return nil, err
	}
	if err = fw.Start(); err != nil {
		return nil, formatError(err)
	}
	defer fw.Close()
	req, err := http.NewRequest(method, fmt.Sprintf("http://%s/%s", fw.Address(), path), nil)
	if err != nil {
		return nil, formatError(err)
	}
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return nil, formatError(err)
	}
	defer closeQuietly(resp.Body)
	out, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, formatError(err)
	}

	return out, nil
}

// ExtractExecResult wraps PodExec and return the execution result and error if has any.
func (c *client) ExtractExecResult(podName, podNamespace, container string, cmd []string) ([]byte, error) {
	stdout, stderr, err := c.PodExec(podName, podNamespace, container, cmd)
	if err != nil {
		if stderr != nil && stderr.String() != "" {
			return nil, fmt.Errorf("error exec'ing into %s/%s %s container: %v\n%s", podName, podNamespace, container, err, stderr.String())
		}
		return nil, fmt.Errorf("error exec'ing into %s/%s %s container: %v", podName, podNamespace, container, err)
	}
	return stdout.Bytes(), nil
}

func (c *client) GetIstioPods(ctx context.Context, namespace string, params map[string]string) ([]v1.Pod, error) {
	if c.revision != "" {
		labelSelector, ok := params["labelSelector"]
		if ok {
			params["labelSelector"] = fmt.Sprintf("%s,%s=%s", labelSelector, label.IstioRev, c.revision)
		} else {
			params["labelSelector"] = fmt.Sprintf("%s=%s", label.IstioRev, c.revision)
		}
	}

	req := c.Get().
		Resource("pods").
		Namespace(namespace)
	for k, v := range params {
		req.Param(k, v)
	}

	res := req.Do(ctx)
	if res.Error() != nil {
		return nil, fmt.Errorf("unable to retrieve Pods: %v", res.Error())
	}
	list := &v1.PodList{}
	if err := res.Into(list); err != nil {
		return nil, fmt.Errorf("unable to parse PodList: %v", res.Error())
	}
	return list.Items, nil
}

type podDetail struct {
	binary    string
	container string
}

func (c *client) GetIstioVersions(ctx context.Context, namespace string) (*version.MeshInfo, error) {
	pods, err := c.GetIstioPods(ctx, namespace, map[string]string{
		"labelSelector": "istio,istio!=ingressgateway,istio!=egressgateway,istio!=ilbgateway",
		"fieldSelector": "status.phase=Running",
	})
	if err != nil {
		return nil, err
	}
	if len(pods) == 0 {
		return nil, fmt.Errorf("no running Istio pods in %q", namespace)
	}

	// exclude data plane components from control plane list
	labelToPodDetail := map[string]podDetail{
		"pilot":            {"/usr/local/bin/pilot-discovery", "discovery"},
		"istiod":           {"/usr/local/bin/pilot-discovery", "discovery"},
		"citadel":          {"/usr/local/bin/istio_ca", "citadel"},
		"galley":           {"/usr/local/bin/galley", "galley"},
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

			stdout, stderr, err := c.PodExec(pod.Name, pod.Namespace, detail.container, cmdJSON)

			if err != nil {
				errs = multierror.Append(errs, fmt.Errorf("error exec'ing into %s %s container: %v", pod.Name, detail.container, err))
				continue
			}

			// At first try parsing stdout
			err = json.Unmarshal(stdout.Bytes(), &v)
			if err == nil && v.ClientVersion.Version != "" {
				info = *v.ClientVersion
			} else {
				// If stdout fails, try the old behavior
				if strings.HasPrefix(stderr.String(), "Error: unknown shorthand flag") {
					stdout, err := c.ExtractExecResult(pod.Name, pod.Namespace, detail.container, cmd)
					if err != nil {
						errs = multierror.Append(errs, fmt.Errorf("error exec'ing into %s %s container: %v", pod.Name, detail.container, err))
						continue
					}

					info, err = version.NewBuildInfoFromOldString(string(stdout))
					if err != nil {
						errs = multierror.Append(errs, fmt.Errorf("error converting server info from JSON: %v", err))
						continue
					}
				} else {
					errs = multierror.Append(errs, fmt.Errorf("error execing into %s %s container: %v", pod.Name, detail.container, stderr.String()))
					continue
				}
			}

			server.Info = info
		}
		res = append(res, server)
	}
	return &res, errs
}

func (c *client) NewPortForwarder(podName, ns, localAddress string, localPort int, podPort int) (PortForwarder, error) {
	return newPortForwarder(c.config, podName, ns, localAddress, localPort, podPort)
}

func (c *client) PodsForSelector(ctx context.Context, namespace, labelSelector string) (*v1.PodList, error) {
	podGet := c.Get().Resource("pods").Namespace(namespace).Param("labelSelector", labelSelector)
	obj, err := podGet.Do(ctx).Get()
	if err != nil {
		return nil, fmt.Errorf("failed retrieving pod: %v", err)
	}
	return obj.(*v1.PodList), nil
}

func closeQuietly(c io.Closer) {
	_ = c.Close()
}
