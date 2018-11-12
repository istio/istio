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
	"net"
	"time"

	"google.golang.org/grpc"

	istioMixerV1 "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/adapter"
	"istio.io/istio/mixer/pkg/server"
	generatedTmplRepo "istio.io/istio/mixer/template"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/deployment"
	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/environments/local"
	"istio.io/istio/pkg/test/framework/environments/local/service"
	"istio.io/istio/pkg/test/framework/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	// LocalComponent is a component for the local environment.
	LocalComponent = &localComponent{}

	retryDelay = retry.Delay(time.Second)
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
func (c *localComponent) Init(ctx environment.ComponentContext, deps map[dependency.Instance]interface{}) (out interface{}, err error) {
	e, ok := ctx.Environment().(*local.Implementation)
	if !ok {
		return nil, fmt.Errorf("unsupported environment: %q", ctx.Environment().EnvironmentID())
	}

	dm := &deployedMixer{
		local:       true,
		environment: ctx.Environment(),
	}

	scopes.CI.Info("=== BEGIN: Starting local Mixer ===")
	defer func() {
		if err != nil {
			scopes.CI.Infof("=== FAILED: Start local Mixer ===")
			_ = dm.Close()
		} else {
			scopes.CI.Infof("=== SUCCEEDED: Start local Mixer ===")
		}
	}()

	dm.workdir, err = e.CreateTmpDirectory("mixer")
	if err != nil {
		return
	}

	dm.attributeManifest, err = deployment.ExtractAttributeManifest(dm.workdir)
	if err != nil {
		return
	}

	dm.args = server.DefaultArgs()
	dm.args.APIPort = 0
	dm.args.MonitoringPort = 0
	dm.args.ConfigStoreURL = fmt.Sprintf("fs://%s", dm.workdir)
	dm.args.Templates = generatedTmplRepo.SupportedTmplInfo
	dm.args.Adapters = adapter.Inventory()

	dm.server, err = server.New(dm.args)
	if err != nil {
		return
	}

	go dm.server.Run()

	conn, err := retry.Do(func() (interface{}, bool, error) {
		conn, err := grpc.Dial(dm.server.Addr().String(), grpc.WithInsecure())
		if err != nil {
			scopes.Framework.Debugf("error connecting to Mixer backend: %v", err)
			return nil, false, err
		}

		return conn, true, nil
	}, retryDelay)
	if err != nil {
		return nil, err
	}
	dm.conns = append(dm.conns, conn.(*grpc.ClientConn))

	client := istioMixerV1.NewMixerClient(conn.(*grpc.ClientConn))
	dm.clients = map[string]istioMixerV1.MixerClient{
		telemetryService: client,
		policyService:    client,
	}

	// Update the mesh with the mixer address
	port := dm.server.Addr().(*net.TCPAddr).Port
	mixerAddr := fmt.Sprintf("%s.%s:%d", localServiceName, service.FullyQualifiedDomainName, port)
	e.Mesh.MixerCheckServer = mixerAddr
	e.Mesh.MixerReportServer = mixerAddr

	// Add a service entry for Mixer.
	_, err = e.ServiceManager.Create(localServiceName, "", model.PortList{
		&model.Port{
			Name:     grpcPortName,
			Protocol: model.ProtocolGRPC,
			Port:     port,
		},
	})
	if err != nil {
		return nil, err
	}

	return dm, nil
}
