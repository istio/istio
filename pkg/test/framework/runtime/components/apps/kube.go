//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package apps

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"testing"

	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/pilot/pkg/model"
	serviceRegistryKube "istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/test/application/echo"
	"istio.io/istio/pkg/test/application/echo/proto"
	"istio.io/istio/pkg/test/deployment"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/api"
	"istio.io/istio/pkg/test/framework/runtime/components/environment/kube"
	testKube "istio.io/istio/pkg/test/kube"

	kubeApiCore "k8s.io/api/core/v1"
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
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: {{ .deployment }}
spec:
  replicas: 1
  template:
    metadata:
      labels:
        app: {{ .service }}
        version: {{ .version }}
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

var (
	defaultKubeApps = []KubeApp{
		{
			Deployment:     "t",
			Service:        "t",
			Version:        "unversioned",
			Port1:          8080,
			Port2:          80,
			Port3:          9090,
			Port4:          90,
			Port5:          7070,
			Port6:          70,
			InjectProxy:    false,
			Headless:       false,
			ServiceAccount: false,
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
			InjectProxy:    true,
			Headless:       false,
			ServiceAccount: false,
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
			InjectProxy:    true,
			Headless:       false,
			ServiceAccount: true,
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
			InjectProxy:    true,
			Headless:       false,
			ServiceAccount: true,
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
			InjectProxy:    true,
			Headless:       false,
			ServiceAccount: true,
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
			InjectProxy:    true,
			Headless:       false,
			ServiceAccount: true,
		},
		{
			Deployment:     "headless",
			Service:        "headless",
			Version:        "unversioned",
			Port1:          80,
			Port2:          8080,
			Port3:          90,
			Port4:          9090,
			Port5:          70,
			Port6:          7070,
			InjectProxy:    true,
			Headless:       true,
			ServiceAccount: true,
		},
	}

	_ components.Apps = &kubeComponent{}
	_ api.Component   = &kubeComponent{}
	_ io.Closer       = &kubeComponent{}
)

func appSelector(serviceName string) string {
	return fmt.Sprintf("%s=%s", appLabel, serviceName)
}

type kubeComponent struct {
	scope        lifecycle.Scope
	requiredApps KubeAppsConfig
	deployments  []*deployment.Instance
	apps         []components.App
	env          *kube.Environment
}

// KubeAppsConfig specifies a list of Kubernetes app we need to deploy in apps component.
type KubeAppsConfig []KubeApp

// String implements String interface required for api.Configuration.
func (ka KubeAppsConfig) String() string {
	return ""
}

// NewKubeComponent factory function for the component
func NewKubeComponent() (api.Component, error) {
	return &kubeComponent{
		apps:        make([]components.App, 0),
		deployments: make([]*deployment.Instance, 0),
	}, nil
}

// Configure implements pkg/test/framework/runtime/api/Configurable interface to allow test suites
// specify customized apps.
func (c *kubeComponent) Configure(config component.Configuration) error {
	apps, ok := config.(KubeAppsConfig)
	if !ok {
		return fmt.Errorf("supplied configuration was not an KubeAppConfig, got %T (%v)", config, config)
	}
	c.requiredApps = apps
	return nil
}

func (c *kubeComponent) Descriptor() component.Descriptor {
	return descriptors.Apps
}

func (c *kubeComponent) Scope() lifecycle.Scope {
	return c.scope
}

func (c *kubeComponent) Start(ctx context.Instance, scope lifecycle.Scope) (err error) {
	c.scope = scope

	env, err := kube.GetEnvironment(ctx)
	if err != nil {
		return err
	}
	c.env = env
	namespace := env.NamespaceForScope(scope)

	// If the test does not explicitly describe the apps it needs, deploy a suite of default apps.
	if len(c.requiredApps) == 0 {
		c.requiredApps = defaultKubeApps
	}

	// Deploy the apps required by the test.
	for _, app := range c.requiredApps {
		d, err := app.newDeployment(env, scope)
		if err != nil {
			return multierror.Prefix(err, fmt.Sprintf("failed creating client for deployment %s: ", app.Deployment))
		}
		c.deployments = append(c.deployments, d)
	}
	for _, app := range c.requiredApps {
		pod, err := app.waitUntilPodIsReady(env, scope)
		if err != nil {
			return multierror.Prefix(err, fmt.Sprintf("failed waiting for deployment %s: ", app.Deployment))
		}
		client, err := newKubeApp(app.Service, namespace, pod, env)
		if err != nil {
			return multierror.Prefix(err, fmt.Sprintf("failed creating client for deployment %s: ", app.Deployment))
		}
		c.apps = append(c.apps, client)
	}
	return nil
}

func (c *kubeComponent) GetApp(name string) (components.App, error) {
	for _, c := range c.apps {
		if c.Name() == name {
			return c, nil
		}
	}

	return nil, fmt.Errorf("unable to locate app for name %s", name)
}

func (c *kubeComponent) GetAppOrFail(name string, t testing.TB) components.App {
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

	// Don't delete the deployments if using Test scope, since the test namespace will be deleted later.
	if c.scope != lifecycle.Test {
		// Delete any deployments
		for i, d := range c.deployments {
			if d != nil {
				err = multierror.Append(err, d.Delete(c.env.Accessor, false)).ErrorOrNil()
				c.deployments[i] = nil
			}
		}

		// Wait for all deployments to be deleted.
		for _, factory := range defaultKubeApps {
			err = multierror.Append(err, factory.waitUntilPodIsDeleted(c.env, c.scope)).ErrorOrNil()
		}
	}
	return
}

type endpoint struct {
	port  *model.Port
	owner *kubeApp
}

func (e *endpoint) Name() string {
	return e.port.Name
}

func (e *endpoint) Owner() components.App {
	return e.owner
}

func (e *endpoint) Protocol() model.Protocol {
	return e.port.Protocol
}

func (e *endpoint) makeURL(opts components.AppCallOptions) *url.URL {
	protocol := string(opts.Protocol)
	switch protocol {
	case components.AppProtocolHTTP:
	case components.AppProtocolGRPC:
	case components.AppProtocolWebSocket:
	default:
		protocol = string(components.AppProtocolHTTP)
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
		Host:   net.JoinHostPort(host, strconv.Itoa(e.port.Port)),
	}
}

type kubeApp struct {
	namespace   string
	serviceName string
	appName     string
	endpoints   []*endpoint
	forwarder   testKube.PortForwarder
	client      *echo.Client
}

func newKubeApp(serviceName, namespace string, pod kubeApiCore.Pod, e *kube.Environment) (out components.App, err error) {
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

	// Extract the endpoints from the service definition.
	a.endpoints = getEndpoints(a, service)

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
	return uint16(commandEndpoints[0].(*endpoint).port.Port), nil
}

func (a *kubeApp) Name() string {
	return a.serviceName
}

func getEndpoints(owner *kubeApp, service *kubeApiCore.Service) []*endpoint {
	out := make([]*endpoint, len(service.Spec.Ports))
	for i, servicePort := range service.Spec.Ports {
		out[i] = &endpoint{
			owner: owner,
			port: &model.Port{
				Name:     servicePort.Name,
				Port:     int(servicePort.Port),
				Protocol: serviceRegistryKube.ConvertProtocol(servicePort.Name, servicePort.Protocol),
			},
		}
	}
	return out
}

func (a *kubeApp) Endpoints() []components.AppEndpoint {
	out := make([]components.AppEndpoint, len(a.endpoints))
	for i, e := range a.endpoints {
		out[i] = e
	}
	return out
}

func (a *kubeApp) EndpointsForProtocol(protocol model.Protocol) []components.AppEndpoint {
	out := make([]components.AppEndpoint, 0)
	for _, e := range a.endpoints {
		if e.Protocol() == protocol {
			out = append(out, e)
		}
	}
	return out
}

// Call implements the environment.DeployedApp interface
func (a *kubeApp) Call(e components.AppEndpoint, opts components.AppCallOptions) ([]*echo.ParsedResponse, error) {
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
	if resp[0].Port != strconv.Itoa(dst.port.Port) {
		return nil, fmt.Errorf("unexpected port: %s", resp[0].Port)
	}

	return resp, nil
}

func (a *kubeApp) CallOrFail(e components.AppEndpoint, opts components.AppCallOptions, t testing.TB) []*echo.ParsedResponse {
	r, err := a.Call(e, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// KubeApp describes the configuration, including port and deployment, service name.
// TODO(incfly): document each option and rename it to be more meaningful.
type KubeApp struct {
	Deployment     string
	Service        string
	Version        string
	Port1          int
	Port2          int
	Port3          int
	Port4          int
	Port5          int
	Port6          int
	InjectProxy    bool
	Headless       bool
	ServiceAccount bool
}

func (d *KubeApp) newDeployment(e *kube.Environment, scope lifecycle.Scope) (*deployment.Instance, error) {
	helmValues := e.HelmValueMap()
	result, err := e.EvaluateWithParams(template, map[string]string{
		"Hub":             helmValues[kube.HubValuesKey],
		"Tag":             helmValues[kube.TagValuesKey],
		"ImagePullPolicy": helmValues[kube.ImagePullPolicyValuesKey],
		"deployment":      d.Deployment,
		"service":         d.Service,
		"app":             d.Service,
		"version":         d.Version,
		"port1":           strconv.Itoa(d.Port1),
		"port2":           strconv.Itoa(d.Port2),
		"port3":           strconv.Itoa(d.Port3),
		"port4":           strconv.Itoa(d.Port4),
		"port5":           strconv.Itoa(d.Port5),
		"port6":           strconv.Itoa(d.Port6),
		"injectProxy":     strconv.FormatBool(d.InjectProxy),
		"headless":        strconv.FormatBool(d.Headless),
		"serviceAccount":  strconv.FormatBool(d.ServiceAccount),
	})
	if err != nil {
		return nil, err
	}

	out := deployment.NewYamlContentDeployment(e.NamespaceForScope(scope), result)
	if err = out.Deploy(e.Accessor, false); err != nil {
		return nil, err
	}
	return out, nil
}

func (d *KubeApp) waitUntilPodIsReady(e *kube.Environment, scope lifecycle.Scope) (kubeApiCore.Pod, error) {
	ns := e.NamespaceForScope(scope)

	podFetchFunc := e.NewSinglePodFetch(ns, appSelector(d.Service), fmt.Sprintf("version=%s", d.Version))
	if err := e.WaitUntilPodsAreReady(podFetchFunc); err != nil {
		return kubeApiCore.Pod{}, err
	}

	pods, err := podFetchFunc()
	if err != nil {
		return kubeApiCore.Pod{}, err
	}
	return pods[0], nil
}

func (d *KubeApp) waitUntilPodIsDeleted(e *kube.Environment, scope lifecycle.Scope) error {
	ns := e.NamespaceForScope(scope)

	podFetchFunc := e.NewPodFetch(ns, appSelector(d.Service), fmt.Sprintf("version=%s", d.Version))
	return e.WaitUntilPodsAreDeleted(podFetchFunc)
}
