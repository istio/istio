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
{{- if .ServiceAccount }}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ .Service }}
---
{{- end }}
apiVersion: v1
kind: Service
metadata:
  name: {{ .Service }}
  labels:
    app: {{ .Service }}
{{- if .ServiceAnnotations }}
    annotations:
{{- range $name, $value := .ServiceAnnotations }}
    - {{ $name }}: {{ printf "%q" $value }}
{{- end }}
{{- end }}
spec:
{{- if .Headless }}
  clusterIP: None
{{- end }}
  ports:
  - port: 80
    targetPort: {{ .Port1 }}
    name: http
  - port: 8080
    targetPort: {{ .Port2 }}
    name: http-two
{{- if .Headless }}
  - port: 10090
    targetPort: {{ .Port3 }}
    name: tcp
{{- else }}
  - port: 90
    targetPort: {{ .Port3 }}
    name: tcp
  - port: 9090
    targetPort: {{ .Port4 }}
    name: https
{{- end }}
  - port: 70
    targetPort: {{ .Port5 }}
    name: http2-example
  - port: 7070
    targetPort: {{ .Port6 }}
    name: grpc
  selector:
    app: {{ .Service }}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Deployment }}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: {{ .Service }}
      version: {{ .Version }}
  template:
    metadata:
      labels:
        app: {{ .Service }}
        version: {{ .Version }}
{{- if ne .Locality "" }}
        istio-Locality: {{ .Locality }}
{{- end }}
{{- if .PodAnnotations }}
    annotations:
{{- range $name, $value := .PodAnnotations }}
    - {{ $name }}: {{ printf "%q" $value }}
{{- end }}
{{- end }}
    spec:
{{- if .ServiceAccount }}
      serviceAccountName: {{ .Service }}
{{- end }}
      containers:
      - name: app
        image: {{ .Hub }}/app:{{ .Tag }}
        imagePullPolicy: {{ .ImagePullPolicy }}
        args:
          - --port
          - "{{ .Port1 }}"
          - --port
          - "{{ .Port2 }}"
          - --port
          - "{{ .Port3 }}"
          - --port
          - "{{ .Port4 }}"
          - --grpc
          - "{{ .Port5 }}"
          - --grpc
          - "{{ .Port6 }}"
          - --port
          - "3333"
          - --version
          - "{{ .Version }}"
        ports:
        - containerPort: {{ .Port1 }}
        - containerPort: {{ .Port2 }}
        - containerPort: {{ .Port3 }}
        - containerPort: {{ .Port4 }}
        - containerPort: {{ .Port5 }}
        - containerPort: {{ .Port6 }}
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
apiVersion: v1
kind: Secret
metadata:
  name: sdstokensecret
type: Opaque
stringData:
  sdstoken: "eyJhbGciOiJSUzI1NiIsImtpZCI6IiJ9.eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2\
VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3BhY2UiOiJkZWZhdWx0Ii\
wia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZWNyZXQubmFtZSI6InZhdWx0LWNpdGFkZWwtc2\
EtdG9rZW4tcmZxZGoiLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW50L3NlcnZpY2UtYWNjb3VudC\
5uYW1lIjoidmF1bHQtY2l0YWRlbC1zYSIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2Vydm\
ljZS1hY2NvdW50LnVpZCI6IjIzOTk5YzY1LTA4ZjMtMTFlOS1hYzAzLTQyMDEwYThhMDA3OSIsInN1Yi\
I6InN5c3RlbTpzZXJ2aWNlYWNjb3VudDpkZWZhdWx0OnZhdWx0LWNpdGFkZWwtc2EifQ.RNH1QbapJKP\
mktV3tCnpiz7hoYpv1TM6LXzThOtaDp7LFpeANZcJ1zVQdys3EdnlkrykGMepEjsdNuT6ndHfh8jRJAZ\
uNWNPGrhxz4BeUaOqZg3v7AzJlMeFKjY_fiTYYd2gBZZxkpv1FvAPihHYng2NeN2nKbiZbsnZNU1qFdv\
bgCISaFqTf0dh75OzgCX_1Fh6HOA7ANf7p522PDW_BRln0RTwUJovCpGeiNCGdujGiNLDZyBcdtikY5r\
y_KXTdrVAcTUvI6lxwRbONNfuN8hrIDl95vJjhUlE-O-_cx8qWtXNdqJlMje1SsiPCL4uq70OepG_I4a\
SzC2o8aDtlQ"
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
			Deployment: "t",
			Service:    "t",
			Version:    "unversioned",
			Port1:      8080,
			Port2:      80,
			Port3:      9090,
			Port4:      90,
			Port5:      7070,
			Port6:      70,
			PodAnnotations: map[string]string{
				"sidecar.istio.io/inject": "false",
			},
			Headless:       false,
			ServiceAccount: false,
			Locality:       "region.zone.subzone",
		},
		{
			Deployment:     "a",
			Service:        "a",
			Version:        "v1",
			Port1:          8080,
			Port2:          80,
			Port3:          9090,
			Port4:          90,
			Port5:          7070,
			Port6:          70,
			Headless:       false,
			ServiceAccount: false,
			Locality:       "region.zone.subzone",
		},
		{
			Deployment:     "b",
			Service:        "b",
			Version:        "unversioned",
			Port1:          80,
			Port2:          8080,
			Port3:          90,
			Port4:          9090,
			Port5:          70,
			Port6:          7070,
			Headless:       false,
			ServiceAccount: true,
			Locality:       "region.zone.subzone",
		},
		{
			Deployment:     "c-v1",
			Service:        "c",
			Version:        "v1",
			Port1:          80,
			Port2:          8080,
			Port3:          90,
			Port4:          9090,
			Port5:          70,
			Port6:          7070,
			Headless:       false,
			ServiceAccount: true,
			Locality:       "region.zone.subzone",
		},
		{
			Deployment:     "c-v2",
			Service:        "c",
			Version:        "v2",
			Port1:          80,
			Port2:          8080,
			Port3:          90,
			Port4:          9090,
			Port5:          70,
			Port6:          7070,
			Headless:       false,
			ServiceAccount: true,
			Locality:       "region.zone.subzone",
		},
		{
			Deployment:     "d",
			Service:        "d",
			Version:        "per-svc-auth",
			Port1:          80,
			Port2:          8080,
			Port3:          90,
			Port4:          9090,
			Port5:          70,
			Port6:          7070,
			Headless:       false,
			ServiceAccount: true,
			Locality:       "region.zone.subzone",
		},
		{
			Deployment:     "Headless",
			Service:        "Headless",
			Version:        "unversioned",
			Port1:          80,
			Port2:          8080,
			Port3:          90,
			Port4:          9090,
			Port5:          70,
			Port6:          7070,
			Headless:       true,
			ServiceAccount: true,
			Locality:       "region.zone.subzone",
		},
	}
)

func appSelector(serviceName string) string {
	return fmt.Sprintf("%s=%s", appLabel, serviceName)
}

func newKube(ctx resource.Context, cfg Config) (Instance, error) {
	if err := cfg.fillInDefaults(ctx); err != nil {
		return nil, err
	}

	env := ctx.Environment().(*kube.Environment)
	c := &kubeComponent{
		apps:        make([]App, 0),
		deployments: make([]*deployment.Instance, 0),
		env:         env,
	}
	c.id = ctx.TrackResource(c)

	var err error

	c.namespace = cfg.Namespace
	if c.namespace == nil {
		if c.namespace, err = namespace.New(ctx, "apps", true); err != nil {
			return nil, err
		}
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
				return nil, fmt.Errorf("failed waiting for Deployment %s: %v", d.Deployment, err)
			}
			client, err := newKubeApp(d.Service, c.namespace.Name(), pod, env)
			if err != nil {
				return nil, fmt.Errorf("failed creating client for Deployment %s: %v", d.Deployment, err)
			}
			c.apps = append(c.apps, client)
		}
		return c, nil
	}

	// Only deploys specified apps.
	dfs := make([]deploymentFactory, len(params))
	for i, param := range params {
		dfs[i] = newDeploymentByAppParm(param)
		d, err := dfs[i].newDeployment(env, c.namespace)
		if err != nil {
			return nil, err
		}
		c.deployments = append(c.deployments, d)
	}
	for _, d := range dfs {
		pod, err := d.waitUntilPodIsReady(env, c.namespace)
		if err != nil {
			return nil, fmt.Errorf("failed waiting for Deployment %s: %v", d.Deployment, err)
		}
		client, err := newKubeApp(d.Service, c.namespace.Name(), pod, env)
		if err != nil {
			return nil, fmt.Errorf("failed creating client for Deployment %s: %v", d.Deployment, err)
		}
		c.apps = append(c.apps, client)
	}

	return c, nil
}

// newDeploymentByAppParm returns a App based on AppParam.
func newDeploymentByAppParm(param AppParam) deploymentFactory {
	return deploymentFactory{
		Deployment:         param.Name,
		Service:            param.Name,
		Locality:           param.Locality,
		PodAnnotations:     param.PodAnnotations,
		ServiceAnnotations: param.ServiceAnnotations,
		Version:            "v1",
		Port1:              8080,
		Port2:              80,
		Port3:              9090,
		Port4:              90,
		Port5:              7070,
		Port6:              70,
		Headless:           false,
		ServiceAccount:     false,
	}
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

	return nil, fmt.Errorf("unable to locate App for name %s", name)
}

func (c *kubeComponent) GetAppOrFail(name string, t testing.TB) App {
	a, err := c.GetApp(name)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (c *kubeComponent) Close() (err error) {
	for _, App := range c.apps {
		err = multierror.Append(err, App.(*kubeApp).Close()).ErrorOrNil()
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
	case AppProtocolTCP:
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
		Path:   opts.Path,
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

	Service, err := e.GetService(namespace, serviceName)
	if err != nil {
		return nil, err
	}

	// Get the App name for this Service.
	a.appName = Service.Labels[appLabel]
	if len(a.appName) == 0 {
		return nil, fmt.Errorf("service does not contain the 'App' label")
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

	// Create a forwarder to the command port of the App.
	a.forwarder, err = e.NewPortForwarder(pod, 0, grpcPort)
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

	// TODO(nmittler): Use an image with the new echo Service and invoke the command port rather than scraping logs.
	// Normalize the count.
	if opts.Count <= 0 {
		opts.Count = 1
	}

	// Forward a request from 'this' Service to the destination Service.
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
	if len(resp) != opts.Count {
		return nil, fmt.Errorf("unexpected number of responses: %d", len(resp))
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
	Hub                string
	Tag                string
	ImagePullPolicy    string
	Deployment         string
	Service            string
	Version            string
	Port1              int
	Port2              int
	Port3              int
	Port4              int
	Port5              int
	Port6              int
	Headless           bool
	ServiceAccount     bool
	Locality           string
	PodAnnotations     map[string]string
	ServiceAnnotations map[string]string
}

func (d *deploymentFactory) renderTemplate() (string, error) {
	s, err := deployment2.SettingsFromCommandLine()
	if err != nil {
		return "", err
	}
	d.Hub = s.Hub
	d.Tag = s.Tag
	d.ImagePullPolicy = s.PullPolicy
	result, err := tmpl.Evaluate(template, d)
	if err != nil {
		return "", err
	}
	return result, nil
}
func (d *deploymentFactory) newDeployment(e *kube.Environment, namespace namespace.Instance) (*deployment.Instance, error) {
	result, err := d.renderTemplate()
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
	podFetchFunc := e.NewSinglePodFetch(ns.Name(), appSelector(d.Service), fmt.Sprintf("Version=%s", d.Version))
	pods, err := e.WaitUntilPodsAreReady(podFetchFunc)
	if err != nil {
		return kubeApiCore.Pod{}, err
	}
	return pods[0], nil
}

func (d *deploymentFactory) waitUntilPodIsDeleted(e *kube.Environment, ns namespace.Instance) error {

	podFetchFunc := e.NewPodFetch(ns.Name(), appSelector(d.Service), fmt.Sprintf("Version=%s", d.Version))
	return e.WaitUntilPodsAreDeleted(podFetchFunc)
}
