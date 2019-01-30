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
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path"
	"testing"
	"time"

	multierror "github.com/hashicorp/go-multierror"

	"google.golang.org/grpc"

	istioMixerV1 "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/adapter"
	"istio.io/istio/mixer/pkg/server"
	generatedTmplRepo "istio.io/istio/mixer/template"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/deployment"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/ids"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/api"
	"istio.io/istio/pkg/test/framework/runtime/components/environment/native"
	"istio.io/istio/pkg/test/framework/runtime/components/environment/native/service"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	retryDelay = retry.Delay(time.Second)

	_ components.Mixer = &nativeComponent{}
	_ api.Component    = &nativeComponent{}
	_ io.Closer        = &nativeComponent{}
)

// NewNativeComponent factory function for the component
func NewNativeComponent() (api.Component, error) {
	return &nativeComponent{}, nil
}

type nativeComponent struct {
	*client
	scope lifecycle.Scope
}

func (c *nativeComponent) Descriptor() component.Descriptor {
	return descriptors.Mixer
}

func (c *nativeComponent) Scope() lifecycle.Scope {
	return c.scope
}

// TODO(nmittler): Remove this.
func (c *nativeComponent) Configure(t testing.TB, _ lifecycle.Scope, cfg string) {
	cfg, err := c.env.Evaluate(cfg)
	if err != nil {
		c.env.DumpState(t.Name())
		t.Fatalf("Error expanding configuration template: %v", err)
	}

	file := path.Join(c.workdir, "config.yaml")
	if err := ioutil.WriteFile(file, []byte(cfg), os.ModePerm); err != nil {
		t.Fatal(err)
	}

	file = path.Join(c.workdir, "attributemanifest.yaml")
	if err := ioutil.WriteFile(file, []byte(c.attributeManifest), os.ModePerm); err != nil {
		t.Fatal(err)
	}

	// TODO: Implement a mechanism for reliably waiting for the configuration to disseminate in the system.
	// We can use CtrlZ to expose the config state of Mixer.
	// See https://github.com/istio/istio/issues/6169 and https://github.com/istio/istio/issues/6170.
	time.Sleep(time.Second * 3)
}

func (c *nativeComponent) Start(ctx context.Instance, scope lifecycle.Scope) (err error) {
	c.scope = scope

	env, err := native.GetEnvironment(ctx)
	if err != nil {
		return err
	}

	c.client = &client{
		local: true,
		env:   env,
	}

	scopes.CI.Info("=== BEGIN: Starting local Mixer ===")
	defer func() {
		if err != nil {
			scopes.CI.Infof("=== FAILED: Start local Mixer ===")
			_ = c.Close()
		} else {
			scopes.CI.Infof("=== SUCCEEDED: Start local Mixer ===")
		}
	}()

	c.client.workdir, err = ctx.CreateTmpDirectory("mixer")
	if err != nil {
		return err
	}

	c.client.attributeManifest, err = deployment.ExtractAttributeManifest(c.client.workdir)
	if err != nil {
		return err
	}

	c.client.args = server.DefaultArgs()
	c.client.args.APIPort = 0
	c.client.args.MonitoringPort = 0
	c.client.args.ConfigStoreURL = fmt.Sprintf("fs://%s", c.client.workdir)
	c.client.args.Templates = generatedTmplRepo.SupportedTmplInfo
	c.client.args.Adapters = adapter.Inventory()

	z := ctx.GetComponent("", ids.Zipkin)
	if z != nil {
		zc, ok := z.(components.Zipkin)
		if ok {
			c.client.args.TracingOptions.ZipkinURL = zc.GetAddress()
			c.client.args.TracingOptions.SamplingRate = 1.0
		}
	}

	c.client.server, err = server.New(c.client.args)
	if err != nil {
		return err
	}

	go c.client.server.Run()

	conn, err := retry.Do(func() (interface{}, bool, error) {
		conn, err := grpc.Dial(c.client.server.Addr().String(), grpc.WithInsecure())
		if err != nil {
			scopes.Framework.Debugf("error connecting to Mixer backend: %v", err)
			return nil, false, err
		}

		return conn, true, nil
	}, retryDelay)
	if err != nil {
		return err
	}
	c.client.conns = append(c.client.conns, conn.(*grpc.ClientConn))

	client := istioMixerV1.NewMixerClient(conn.(*grpc.ClientConn))
	c.client.clients = map[string]istioMixerV1.MixerClient{
		telemetryService: client,
		policyService:    client,
	}

	// Update the mesh with the mixer address
	port := c.client.server.Addr().(*net.TCPAddr).Port
	mixerAddr := fmt.Sprintf("%s.%s:%d", localServiceName, service.FullyQualifiedDomainName, port)
	env.Mesh.MixerCheckServer = mixerAddr
	env.Mesh.MixerReportServer = mixerAddr

	// Add a service entry for Mixer.
	_, err = env.ServiceManager.Create(localServiceName, "", model.PortList{
		&model.Port{
			Name:     grpcPortName,
			Protocol: model.ProtocolGRPC,
			Port:     port,
		},
	})
	if err != nil {
		return err
	}

	return
}

func (c *nativeComponent) Close() (err error) {
	if c.client != nil {
		err = multierror.Append(err, c.client.Close()).ErrorOrNil()
		c.client = nil
	}
	return
}
