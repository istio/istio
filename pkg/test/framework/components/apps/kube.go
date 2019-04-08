// Copyright 2019 Istio Authors
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

package apps

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"testing"

	"github.com/hashicorp/go-multierror"
	kubeApiCore "k8s.io/api/core/v1"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pilot/pkg/model"
	serviceRegistryKube "istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/test/application/echo"
	"istio.io/istio/pkg/test/application/echo/proto"
	"istio.io/istio/pkg/test/deployment"
	deployment2 "istio.io/istio/pkg/test/framework/components/deployment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	testKube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/tmpl"
)

const (
	appLabel = "app"

	template = `
{{- if eq .serviceAccount "true" }}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ .service }}
---
{{- end }}
apiVersion: v1
kind: Service
metadata:
  name: {{ .service }}
  labels:
    app: {{ .service }}
spec:
{{- if eq .headless "true" }}
  clusterIP: None
{{- end }}
  ports:
  - port: 80
    targetPort: {{ .port1 }}
    name: http
  - port: 8080
    targetPort: {{ .port2 }}
    name: http-two
{{- if eq .headless "true" }}
  - port: 10090
    targetPort: {{ .port3 }}
    name: tcp
{{- else }}
  - port: 90
    targetPort: {{ .port3 }}
    name: tcp
  - port: 9090
    targetPort: {{ .port4 }}
    name: https
{{- end }}
  - port: 70
    targetPort: {{ .port5 }}
    name: http2-example
  - port: 7070
    targetPort: {{ .port6 }}
    name: grpc
  selector:
    app: {{ .service }}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .deployment }}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: {{ .service }}
      version: {{ .version }}
      istio-locality: {{ .locality }}
  template:
    metadata:
      labels:
        app: {{ .service }}
        version: {{ .version }}
        istio-locality: {{ .locality }}
{{- if eq .injectProxy "false" }}
      annotations:
        sidecar.istio.io/inject: "false"
{{- end }}
    spec:
{{- if eq .serviceAccount "true" }}
      serviceAccountName: {{ .service }}
{{- end }}
      containers:
      - name: app
        image: {{ .Hub }}/app:{{ .Tag }}
        imagePullPolicy: {{ .ImagePullPolicy }}
        args:
          - --port
          - "{{ .port1 }}"
          - --port
          - "{{ .port2 }}"
          - --port
          - "{{ .port3 }}"
          - --port
          - "{{ .port4 }}"
          - --grpc
          - "{{ .port5 }}"
          - --grpc
          - "{{ .port6 }}"
          - --port
          - "3333"
          - --version
          - "{{ .version }}"
        ports:
        - containerPort: {{ .port1 }}
        - containerPort: {{ .port2 }}
        - containerPort: {{ .port3 }}
        - containerPort: {{ .port4 }}
        - containerPort: {{ .port5 }}
        - containerPort: {{ .port6 }}
        - name: tcp-health-port
          containerPort: 3333
        readinessProbe:
          httpGet:
            path: /
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 10
          failureThreshold: 10
        livenessProbe:
          tcpSocket:
            port: tcp-health-port
          initialDelaySeconds: 10
          periodSeconds: 10
          failureThreshold: 10
---
`
)

type kubeComponent struct {
	id resource.ID

	deployments []*deployment.Instance
	apps        []App
	env         *kube.Environment

	namespace namespace.Instance
}

var (
	_ Instance  = &kubeComponent{}
	_ io.Closer = &kubeComponent{}

	deploymentFactories = []*deploymentFactory{
		{
			deployment:     "t",
			service:        "t",
			version:        "unversioned",
			port1:          8080,
			port2:          80,
			port3:          9090,
			port4:          90,
			port5:          7070,
			port6:          70,
			injectProxy:    false,
			headless:       false,
			serviceAccount: false,
			locality:       "region.zone.subzone",
		},
		{
			deployment:     "a",
			service:        "a",
			version:        "v1",
			port1:          8080,
			port2:          80,
			port3:          9090,
			port4:          90,
			port5:          7070,
			port6:          70,
			injectProxy:    true,
			headless:       false,
			serviceAccount: false,
			locality:       "region.zone.subzone",
		},
		{
			deployment:     "b",
			service:        "b",
			version:        "unversioned",
			port1:          80,
			port2:          8080,
			port3:          90,
			port4:          9090,
			port5:          70,
			port6:          7070,
			injectProxy:    true,
			headless:       false,
			serviceAccount: true,
			locality:       "region.zone.subzone",
		},
		{
			deployment:     "c-v1",
			service:        "c",
			version:        "v1",
			port1:          80,
			port2:          8080,
			port3:          90,
			port4:          9090,
			port5:          70,
			port6:          7070,
			injectProxy:    true,
			headless:       false,
			serviceAccount: true,
			locality:       "region.zone.subzone",
		},
		{
			deployment:     "c-v2",
			service:        "c",
			version:        "v2",
			port1:          80,
			port2:          8080,
			port3:          90,
			port4:          9090,
			port5:          70,
			port6:          7070,
			injectProxy:    true,
			headless:       false,
			serviceAccount: true,
			locality:       "region.zone.subzone",
		},
		{
			deployment:     "d",
			service:        "d",
			version:        "per-svc-auth",
			port1:          80,
			port2:          8080,
			port3:          90,
			port4:          9090,
			port5:          70,
			port6:          7070,
			injectProxy:    true,
			headless:       false,
			serviceAccount: true,
			locality:       "region.zone.subzone",
		},
		{
			deployment:     "headless",
			service:        "headless",
			version:        "unversioned",
			port1:          80,
			port2:          8080,
			port3:          90,
			port4:          9090,
			port5:          70,
			port6:          7070,
			injectProxy:    true,
			headless:       true,
			serviceAccount: true,
			locality:       "region.zone.subzone",
		},
	}
)

func appSelector(serviceName string) string {
	return fmt.Sprintf("%s=%s", appLabel, serviceName)
}

func newKube(ctx resource.Context, cfg Config) (Instance, error) {
	env := ctx.Environment().(*kube.Environment)
	c := &kubeComponent{
		apps:        make([]App, 0),
		deployments: make([]*deployment.Instance, 0),
		env:         env,
	}
	c.id = ctx.TrackResource(c)

	var err error

	// Wait for the pods to transition to running.
	if c.namespace, err = namespace.New(ctx, "apps", true); err != nil {
		return nil, err
	}

	params := cfg.AppParams
	if len(params) == 0 {
		// Apply all the configs for the deployments.
		for _, factory := range deploymentFactories {
			d, err := factory.newDeployment(env, c.namespace)
			if err != nil {
				return nil, err
			}
			c.deployments = append(c.deployments, d)
		}

		for _, d := range deploymentFactories {
			pod, err := d.waitUntilPodIsReady(env, c.namespace)
			if err != nil {
				return nil, fmt.Errorf("failed waiting for deployment %s: %v", d.deployment, err)
			}
			client, err := newKubeApp(d.service, c.namespace.Name(), pod, env)
			if err != nil {
				return nil, fmt.Errorf("failed creating client for deployment %s: %v", d.deployment, err)
			}
			c.apps = append(c.apps, client)
		}
		return c, nil
	}

	// Only deploys specified apps.
	dfs := make([]deploymentFactory, len(params))
	for i, param := range params {
		dfs[i] = deploymentFactory{
			deployment:     param.Name,
			service:        param.Name,
			locality:       param.Locality,
			version:        "v1",
			port1:          8080,
			port2:          80,
			port3:          9090,
			port4:          90,
			port5:          7070,
			port6:          70,
			injectProxy:    true,
			headless:       false,
			serviceAccount: false,
		}
		d, err := dfs[i].newDeployment(env, c.namespace)
		if err != nil {
			return nil, err
		}
		c.deployments = append(c.deployments, d)
	}
	for _, d := range dfs {
		pod, err := d.waitUntilPodIsReady(env, c.namespace)
		if err != nil {
			return nil, fmt.Errorf("failed waiting for deployment %s: %v", d.deployment, err)
		}
		client, err := newKubeApp(d.service, c.namespace.Name(), pod, env)
		if err != nil {
			return nil, fmt.Errorf("failed creating client for deployment %s: %v", d.deployment, err)
		}
		c.apps = append(c.apps, client)
	}

	return c, nil
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

func (c *kubeComponent) Namespace() namespace.Instance {
	return c.namespace
}

func (c *kubeComponent) GetApp(name string) (App, error) {
	for _, c := range c.apps {
		if c.Name() == name {
			return c, nil
		}
	}

	return nil, fmt.Errorf("unable to locate app for name %s", name)
}

func (c *kubeComponent) GetAppOrFail(name string, t testing.TB) App {
	a, err := c.GetApp(name)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (c *kubeComponent) Close() (err error) {
	for _, app := range c.apps {
		err = multierror.Append(err, app.(*kubeApp).Close()).ErrorOrNil()
	}

	// Delete any deployments
	for i, d := range c.deployments {
		if d != nil {
			err = multierror.Append(err, d.Delete(c.env.Accessor, false)).ErrorOrNil()
			c.deployments[i] = nil
		}
	}

	// Wait for all deployments to be deleted.
	for _, factory := range deploymentFactories {
		err = multierror.Append(err, factory.waitUntilPodIsDeleted(c.env, c.namespace)).ErrorOrNil()
	}

	return
}

type endpoint struct {
	networkEndpoint model.NetworkEndpoint
	owner           *kubeApp
}

func (e *endpoint) Name() string {
	return e.networkEndpoint.ServicePort.Name
}

func (e *endpoint) Owner() App {
	return e.owner
}

func (e *endpoint) Protocol() model.Protocol {
	return e.networkEndpoint.ServicePort.Protocol
}

func (e *endpoint) NetworkEndpoint() model.NetworkEndpoint {
	return e.networkEndpoint
}

func (e *endpoint) makeURL(opts AppCallOptions) *url.URL {
	protocol := string(opts.Protocol)
	switch protocol {
	case AppProtocolHTTP:
	case AppProtocolGRPC:
	case AppProtocolWebSocket:
	default:
		protocol = string(AppProtocolHTTP)
	}

	if opts.Secure {
		protocol += "s"
	}

	host := e.owner.serviceName
	if !opts.UseShortHostname {
		host += "." + e.owner.namespace
	}
	return &url.URL{
		Scheme: protocol,
		Host:   net.JoinHostPort(host, strconv.Itoa(e.networkEndpoint.ServicePort.Port)),
	}
}

// Represents a deployed App in k8s environment.
type KubeApp interface {
	App
	EndpointForPort(port int) AppEndpoint
}
type kubeApp struct {
	namespace   string
	serviceName string
	appName     string
	endpoints   []*endpoint
	forwarder   testKube.PortForwarder
	client      *echo.Client
}

var _ App = &kubeApp{}

func newKubeApp(serviceName, namespace string, pod kubeApiCore.Pod, e *kube.Environment) (out App, err error) {
	a := &kubeApp{
		serviceName: serviceName,
		namespace:   namespace,
	}
	defer func() {
		if err != nil {
			_ = a.Close()
		}
	}()

	service, err := e.GetService(namespace, serviceName)
	if err != nil {
		return nil, err
	}

	// Get the app name for this service.
	a.appName = service.Labels[appLabel]
	if len(a.appName) == 0 {
		return nil, fmt.Errorf("service does not contain the 'app' label")
	}

	eps, err := e.GetEndpoints(namespace, serviceName, kubeApiMeta.GetOptions{})
	if err != nil {
		return nil, err
	}

	// Extract the endpoints from the endpoints definition.
	a.endpoints = getEndpoints(a, eps)

	var grpcPort uint16
	grpcPort, err = a.getGrpcPort()
	if err != nil {
		return nil, err
	}

	// Create a forwarder to the command port of the app.
	a.forwarder, err = e.NewPortForwarder(&testKube.PodSelectOptions{
		PodNamespace: pod.Namespace,
		PodName:      pod.Name,
	}, 0, grpcPort)
	if err != nil {
		return nil, err
	}
	if err = a.forwarder.Start(); err != nil {
		return nil, err
	}

	a.client, err = echo.NewClient(a.forwarder.Address())
	out = a
	return
}

func (a *kubeApp) Close() (err error) {
	if a.client != nil {
		err = multierror.Append(err, a.client.Close()).ErrorOrNil()
	}
	if a.forwarder != nil {
		err = multierror.Append(err, a.forwarder.Close()).ErrorOrNil()
	}
	return
}

func (a *kubeApp) getGrpcPort() (uint16, error) {
	commandEndpoints := a.EndpointsForProtocol(model.ProtocolGRPC)
	if len(commandEndpoints) == 0 {
		return 0, fmt.Errorf("unable fo find GRPC command port")
	}
	return uint16(commandEndpoints[0].(*endpoint).networkEndpoint.ServicePort.Port), nil
}

func (a *kubeApp) Name() string {
	return a.serviceName
}

func getEndpoints(owner *kubeApp, endpoints *kubeApiCore.Endpoints) []*endpoint {
	out := make([]*endpoint, 0)
	for _, subset := range endpoints.Subsets {
		for _, address := range subset.Addresses {
			for _, port := range subset.Ports {
				out = append(out, &endpoint{
					owner: owner,
					networkEndpoint: model.NetworkEndpoint{
						Address: address.IP,
						Port:    int(port.Port),
						ServicePort: &model.Port{
							Name:     port.Name,
							Port:     int(port.Port),
							Protocol: serviceRegistryKube.ConvertProtocol(port.Name, port.Protocol),
						},
					},
				})
			}
		}
	}
	return out
}

func (a *kubeApp) Endpoints() []AppEndpoint {
	out := make([]AppEndpoint, len(a.endpoints))
	for i, e := range a.endpoints {
		out[i] = e
	}
	return out
}

func (a *kubeApp) EndpointsForProtocol(protocol model.Protocol) []AppEndpoint {
	out := make([]AppEndpoint, 0)
	for _, e := range a.endpoints {
		if e.Protocol() == protocol {
			out = append(out, e)
		}
	}
	return out
}

func (a *kubeApp) EndpointForPort(port int) AppEndpoint {
	for _, e := range a.endpoints {
		if e.networkEndpoint.ServicePort.Port == port {
			return e
		}
	}
	return nil
}

// Call implements the environment.DeployedApp interface
func (a *kubeApp) Call(e AppEndpoint, opts AppCallOptions) ([]*echo.ParsedResponse, error) {
	dst, ok := e.(*endpoint)
	if !ok {
		return nil, fmt.Errorf("supplied endpoint was not created by this environment")
	}

	// Normalize the count.
	if opts.Count <= 0 {
		opts.Count = 1
	}

	// TODO(nmittler): Use an image with the new echo service and invoke the command port rather than scraping logs.
	// Normalize the count.
	if opts.Count <= 0 {
		opts.Count = 1
	}

	// Forward a request from 'this' service to the destination service.
	dstURL := dst.makeURL(opts)
	dstServiceName := dst.owner.Name()

	// If host header is set, override the destination with it
	if opts.Headers.Get("Host") != "" {
		dstServiceName = opts.Headers.Get("Host")
	}

	resp, err := a.client.ForwardEcho(&proto.ForwardEchoRequest{
		Url:   dstURL.String(),
		Count: int32(opts.Count),
		Headers: []*proto.Header{
			{
				Key:   "Host",
				Value: dstServiceName,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	if len(resp) != 1 {
		return nil, fmt.Errorf("unexpected number of responses: %d", len(resp))
	}
	if !resp[0].IsOK() {
		return nil, fmt.Errorf("unexpected response status code: %s", resp[0].Code)
	}
	if resp[0].Host != dstServiceName {
		return nil, fmt.Errorf("unexpected host: %s", resp[0].Host)
	}
	if resp[0].Port != strconv.Itoa(dst.networkEndpoint.ServicePort.Port) {
		return nil, fmt.Errorf("unexpected port: %s", resp[0].Port)
	}

	return resp, nil
}

func (a *kubeApp) CallOrFail(e AppEndpoint, opts AppCallOptions, t testing.TB) []*echo.ParsedResponse {
	r, err := a.Call(e, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

type deploymentFactory struct {
	deployment     string
	service        string
	version        string
	port1          int
	port2          int
	port3          int
	port4          int
	port5          int
	port6          int
	injectProxy    bool
	headless       bool
	serviceAccount bool
	locality       string
}

func (d *deploymentFactory) newDeployment(e *kube.Environment, namespace namespace.Instance) (*deployment.Instance, error) {

	s, err := deployment2.SettingsFromCommandLine()
	if err != nil {
		return nil, err
	}

	result, err := tmpl.Evaluate(template, map[string]string{
		"Hub":             s.Hub,
		"Tag":             s.Tag,
		"ImagePullPolicy": s.PullPolicy,
		"deployment":      d.deployment,
		"service":         d.service,
		"app":             d.service,
		"version":         d.version,
		"port1":           strconv.Itoa(d.port1),
		"port2":           strconv.Itoa(d.port2),
		"port3":           strconv.Itoa(d.port3),
		"port4":           strconv.Itoa(d.port4),
		"port5":           strconv.Itoa(d.port5),
		"port6":           strconv.Itoa(d.port6),
		"healthPort":      "true",
		"injectProxy":     strconv.FormatBool(d.injectProxy),
		"headless":        strconv.FormatBool(d.headless),
		"serviceAccount":  strconv.FormatBool(d.serviceAccount),
		"locality":        d.locality,
	})
	if err != nil {
		return nil, err
	}

	out := deployment.NewYamlContentDeployment(namespace.Name(), result)
	if err = out.Deploy(e.Accessor, false); err != nil {
		return nil, err
	}
	return out, nil
}

func (d *deploymentFactory) waitUntilPodIsReady(e *kube.Environment, ns namespace.Instance) (kubeApiCore.Pod, error) {
	podFetchFunc := e.NewSinglePodFetch(ns.Name(), appSelector(d.service), fmt.Sprintf("version=%s", d.version))
	if err := e.WaitUntilPodsAreReady(podFetchFunc); err != nil {
		return kubeApiCore.Pod{}, err
	}

	pods, err := podFetchFunc()
	if err != nil {
		return kubeApiCore.Pod{}, err
	}
	return pods[0], nil
}

func (d *deploymentFactory) waitUntilPodIsDeleted(e *kube.Environment, ns namespace.Instance) error {

	podFetchFunc := e.NewPodFetch(ns.Name(), appSelector(d.service), fmt.Sprintf("version=%s", d.version))
	return e.WaitUntilPodsAreDeleted(podFetchFunc)
}
