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
	"io"
	"net"
	"testing"
	"time"

	multierror "github.com/hashicorp/go-multierror"
	"google.golang.org/grpc"

	istioMixerV1 "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/adapter"
	"istio.io/istio/mixer/pkg/server"
	generatedTmplRepo "istio.io/istio/mixer/template"
	"istio.io/istio/pkg/test/deployment"
	"istio.io/istio/pkg/test/framework/components/environment/native"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	retryDelay = retry.Delay(time.Second)
)

type nativeComponent struct {
	id     resource.ID
	ctx    resource.Context
	env    *native.Environment
	galley galley.Instance

	*client
}

var _ Instance = &nativeComponent{}
var _ io.Closer = &nativeComponent{}

func newNative(ctx resource.Context, config Config) (Instance, error) {
	n := &nativeComponent{
		ctx:    ctx,
		env:    ctx.Environment().(*native.Environment),
		galley: config.Galley,
	}
	n.id = ctx.TrackResource(n)

	n.client = &client{
		local: true,
		env:   ctx.Environment().(*native.Environment),
	}

	var err error
	scopes.CI.Info("=== BEGIN: Starting local Mixer ===")
	defer func() {
		if err != nil {
			scopes.CI.Infof("=== FAILED: Start local Mixer ===")
			_ = n.Close()
		} else {
			scopes.CI.Infof("=== SUCCEEDED: Start local Mixer ===")
		}
	}()

	n.client.workdir, err = ctx.CreateTmpDirectory("mixer")
	if err != nil {
		return nil, err
	}

	helmExtractDir, err := ctx.CreateTmpDirectory("helm-mixer-attribute-extract")
	if err != nil {
		return nil, err
	}
	n.client.attributeManifest, err = deployment.ExtractAttributeManifest(helmExtractDir)
	if err != nil {
		return nil, err
	}

	n.client.args = server.DefaultArgs()
	n.client.args.APIPort = 0
	n.client.args.MonitoringPort = 0
	n.client.args.ConfigStoreURL = "mcp://" + config.Galley.Address()[6:]
	n.client.args.Templates = generatedTmplRepo.SupportedTmplInfo
	n.client.args.Adapters = adapter.Inventory()

	n.client.server, err = server.New(n.client.args)
	if err != nil {
		return nil, err
	}

	go n.client.server.Run()

	conn, err := retry.Do(func() (interface{}, bool, error) {
		conn, err := grpc.Dial(n.client.server.Addr().String(), grpc.WithInsecure())
		if err != nil {
			scopes.Framework.Debugf("error connecting to Mixer backend: %v", err)
			return nil, false, err
		}

		return conn, true, nil
	}, retryDelay)

	if err != nil {
		return nil, err
	}
	n.client.conns = append(n.client.conns, conn.(*grpc.ClientConn))

	client := istioMixerV1.NewMixerClient(conn.(*grpc.ClientConn))
	n.client.clients = map[string]istioMixerV1.MixerClient{
		telemetryService: client,
		policyService:    client,
	}

	//// Update the mesh with the mixer address
	//port := n.client.server.Addr().(*net.TCPAddr).Port
	//mixerAddr := fmt.Sprintf("%s.%s:%d", localServiceName, service.FullyQualifiedDomainName, port)
	//env.Mesh.MixerCheckServer = mixerAddr
	//env.Mesh.MixerReportServer = mixerAddr
	//
	//// Add a service entry for Mixer.
	//_, err = env.ServiceManager.Create(localServiceName, "", model.PortList{
	//	&model.Port{
	//		Name:     grpcPortName,
	//		Protocol: model.ProtocolGRPC,
	//		Port:     port,
	//	},
	//})
	if err != nil {
		return nil, err
	}

	return n, nil
}

func (c *nativeComponent) ID() resource.ID {
	return c.id
}

func (c *nativeComponent) Report(t testing.TB, attributes map[string]interface{}) {
	c.client.Report(t, attributes)
}

func (c *nativeComponent) Check(t testing.TB, attributes map[string]interface{}) CheckResponse {
	return c.client.Check(t, attributes)
}

func (c *nativeComponent) GetCheckAddress() net.Addr {
	return c.client.server.Addr()
}

func (c *nativeComponent) GetReportAddress() net.Addr {
	return c.client.server.Addr()
}

func (c *nativeComponent) Close() (err error) {
	if c.client != nil {
		err = multierror.Append(err, c.client.Close()).ErrorOrNil()
		c.client = nil
	}
	return
}
