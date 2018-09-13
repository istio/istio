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

package mixer

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/go-multierror"

	"go.uber.org/multierr"
	"google.golang.org/grpc"

	istio_mixer_v1 "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/server"
	generatedTmplRepo "istio.io/istio/mixer/template"
	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/environments/kubernetes"
	"istio.io/istio/pkg/test/framework/environments/local"
	"istio.io/istio/pkg/test/framework/internal"
	"istio.io/istio/pkg/test/kube"
)

const (
	telemetryService = "telemetry"
	policyService    = "policy"
	grpcPortName     = "grpc-mixer"
)

var (
	// LocalComponent is a component for the local environment.
	LocalComponent = &localComponent{}

	// KubeComponent is a component for the Kubernetes environment.
	KubeComponent = &kubeComponent{}

	_ environment.DeployedMixer = &deployedMixer{}
	_ internal.Configurable     = &deployedMixer{}
	_ io.Closer                 = &deployedMixer{}
)

type localComponent struct {
}

// ID implements the component.Component interface.
func (c *localComponent) ID() dependency.Instance {
	return dependency.Mixer
}

// Requires implements the component.Component interface.
func (c *localComponent) Requires() []dependency.Instance {
	return make([]dependency.Instance, 0)
}

// Init implements the component.Component interface.
func (c *localComponent) Init(ctx environment.ComponentContext, deps map[dependency.Instance]interface{}) (interface{}, error) {
	e, ok := ctx.Environment().(*local.Implementation)
	if !ok {
		return nil, fmt.Errorf("unsupported environment: %q", ctx.Environment().EnvironmentID())
	}

	dir, err := e.CreateTmpDirectory("mixer")
	if err != nil {
		return nil, err
	}

	args := server.DefaultArgs()
	args.APIPort = 0
	args.MonitoringPort = 0
	args.ConfigStoreURL = fmt.Sprintf("fs://%s", dir)

	args.Templates = generatedTmplRepo.SupportedTmplInfo
	args.Adapters = adapter.Inventory()

	mi, err := server.New(args)
	if err != nil {
		return nil, err
	}

	go mi.Run()

	conn, err := grpc.Dial(mi.Addr().String(), grpc.WithInsecure())
	if err != nil {
		_ = mi.Close()
		return nil, err
	}

	client := istio_mixer_v1.NewMixerClient(conn)

	return &deployedMixer{
		local: true,
		conn:  conn,
		clients: map[string]istio_mixer_v1.MixerClient{
			telemetryService: client,
			policyService:    client,
		},
		args:    args,
		server:  mi,
		workdir: dir,
	}, nil
}

type kubeComponent struct {
}

// ID implements the component.Component interface.
func (c *kubeComponent) ID() dependency.Instance {
	return dependency.Mixer
}

// Requires implements the component.Component interface.
func (c *kubeComponent) Requires() []dependency.Instance {
	return make([]dependency.Instance, 0)
}

// Init implements the component.Component interface.
func (c *kubeComponent) Init(ctx environment.ComponentContext, deps map[dependency.Instance]interface{}) (interface{}, error) {
	e, ok := ctx.Environment().(*kubernetes.Implementation)
	if !ok {
		return nil, fmt.Errorf("unsupported environment: %q", ctx.Environment().EnvironmentID())
	}

	result, err := c.doInit(e)
	if err != nil {
		return nil, multierror.Prefix(err, "mixer init failed:")
	}
	return result, nil
}

func (c *kubeComponent) doInit(e *kubernetes.Implementation) (interface{}, error) {
	res := &deployedMixer{
		local: false,
		// Use the DefaultArgs to get config identity attribute
		args:    server.DefaultArgs(),
		clients: make(map[string]istio_mixer_v1.MixerClient),
	}

	s := e.KubeSettings()
	for _, serviceType := range []string{telemetryService, policyService} {
		pod, err := e.Accessor.WaitForPodBySelectors(s.IstioSystemNamespace, "istio=mixer", "istio-mixer-type="+serviceType)
		if err != nil {
			return nil, err
		}

		port, err := getGrpcPort(e, serviceType)
		if err != nil {
			return nil, err
		}

		options := &kube.PodSelectOptions{
			PodNamespace: pod.Namespace,
			PodName:      pod.Name,
		}
		forwarder, err := kube.PortForward(s.KubeConfig, options, "", strconv.Itoa(port))
		if err != nil {
			return nil, err
		}

		conn, err := grpc.Dial(forwarder.Address(), grpc.WithInsecure())
		if err != nil {
			return nil, err
		}

		client := istio_mixer_v1.NewMixerClient(conn)
		res.clients[serviceType] = client
		res.forwarders = append(res.forwarders, forwarder)
	}

	return res, nil
}

func getGrpcPort(e *kubernetes.Implementation, serviceType string) (int, error) {
	svc, err := e.Accessor.GetService(e.KubeSettings().IstioSystemNamespace, "istio-"+serviceType)
	if err != nil {
		return 0, fmt.Errorf("failed to retrieve service %s: %v", serviceType, err)
	}
	for _, portInfo := range svc.Spec.Ports {
		if portInfo.Name == grpcPortName {
			return portInfo.TargetPort.IntValue(), nil
		}
	}
	return 0, fmt.Errorf("failed to get target port in service %s", serviceType)
}

type deployedMixer struct {
	// Indicates that the component is running in local mode.
	local bool

	conn    *grpc.ClientConn
	clients map[string]istio_mixer_v1.MixerClient

	args    *server.Args
	server  *server.Server
	workdir string

	forwarders []kube.PortForwarder
}

// Report implements DeployedMixer.Report.
func (d *deployedMixer) Report(t testing.TB, attributes map[string]interface{}) {
	t.Helper()

	req := istio_mixer_v1.ReportRequest{
		Attributes: []istio_mixer_v1.CompressedAttributes{
			getAttrBag(attributes)},
	}
	_, err := d.clients[telemetryService].Report(context.Background(), &req)

	if err != nil {
		t.Fatalf("Error sending report: %v", err)
	}
}

// Check implements DeployedMixer.Check.
func (d *deployedMixer) Check(t testing.TB, attributes map[string]interface{}) environment.CheckResponse {
	t.Helper()

	req := istio_mixer_v1.CheckRequest{
		Attributes: getAttrBag(attributes),
	}
	response, err := d.clients[policyService].Check(context.Background(), &req)

	if err != nil {
		t.Fatalf("Error sending check: %v", err)
	}

	return environment.CheckResponse{
		Raw: response,
	}
}

// ApplyConfig implements Configurable.ApplyConfig.
func (d *deployedMixer) ApplyConfig(cfg string) error {
	// This only applies when Mixer is running locally.
	if d.local {
		file := path.Join(d.workdir, "config.yaml")
		err := ioutil.WriteFile(file, []byte(cfg), os.ModePerm)

		if err == nil {
			// TODO: Implement a mechanism for reliably waiting for the configuration to disseminate in the system.
			// We can use CtrlZ to expose the config state of Mixer.
			// See https://github.com/istio/istio/issues/6169 and https://github.com/istio/istio/issues/6170.
			time.Sleep(time.Second * 3)
		}

		return err
	}

	// We shouldn't getting an ApplyConfig for the Kubernetes case.
	return fmt.Errorf("unexpected ApplyConfig call to Mixer component for Kubernetes environment: %s", cfg)
}

// Close implements io.Closer.
func (d *deployedMixer) Close() error {
	var err error
	if d.conn != nil {
		err = multierr.Append(err, d.conn.Close())
		d.conn = nil
	}

	if d.server != nil {
		err = multierr.Append(err, d.server.Close())
		d.server = nil
	}

	for _, fw := range d.forwarders {
		fw.Close()
	}

	return err
}

func getAttrBag(attrs map[string]interface{}) istio_mixer_v1.CompressedAttributes {
	requestBag := attribute.GetMutableBag(nil)
	for k, v := range attrs {
		requestBag.Set(k, v)
	}

	var attrProto istio_mixer_v1.CompressedAttributes
	requestBag.ToProto(&attrProto, nil, 0)
	return attrProto
}
