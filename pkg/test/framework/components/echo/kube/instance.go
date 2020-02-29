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

package kube

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	appEcho "istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	kubeEnv "istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/resource"

	kubeCore "k8s.io/api/core/v1"
)

const (
	tcpHealthPort     = 3333
	httpReadinessPort = 8080
	defaultDomain     = "cluster.local"
)

var (
	_ echo.Instance = &instance{}
	_ io.Closer     = &instance{}
)

type instance struct {
	id        resource.ID
	cfg       echo.Config
	clusterIP string
	env       *kubeEnv.Environment
	workloads []*workload
	grpcPort  uint16
	ctx       resource.Context
}

func newInstance(ctx resource.Context, cfg echo.Config) (out *instance, err error) {
	// Fill in defaults for any missing values.
	common.AddPortIfMissing(&cfg, protocol.GRPC)
	if err = common.FillInDefaults(ctx, defaultDomain, &cfg); err != nil {
		return nil, err
	}

	// Validate the configuration.
	if cfg.Galley == nil {
		// Galley is not actually required currently, but it will be once Pilot gets
		// all resources from Galley. Requiring now for forward-compatibility.
		return nil, errors.New("galley must be provided")
	}

	env := ctx.Environment().(*kubeEnv.Environment)
	c := &instance{
		env: env,
		cfg: cfg,
		ctx: ctx,
	}
	c.id = ctx.TrackResource(c)

	// Save the GRPC port.
	grpcPort := common.GetPortForProtocol(&cfg, protocol.GRPC)
	if grpcPort == nil {
		return nil, errors.New("unable fo find GRPC command port")
	}
	c.grpcPort = uint16(grpcPort.InstancePort)

	// Generate the deployment YAML.
	generatedYAML, err := generateYAML(cfg)
	if err != nil {
		return nil, err
	}

	// Deploy the YAML.
	if err = env.ApplyContents(cfg.Namespace.Name(), generatedYAML); err != nil {
		return nil, err
	}

	// Now retrieve the service information to find the ClusterIP
	s, err := env.GetService(cfg.Namespace.Name(), cfg.Service)
	if err != nil {
		return nil, err
	}

	c.clusterIP = s.Spec.ClusterIP
	switch c.clusterIP {
	case kubeCore.ClusterIPNone, "":
		if !cfg.Headless {
			return nil, fmt.Errorf("invalid ClusterIP %s for non-headless service %s/%s",
				c.clusterIP,
				c.cfg.Namespace.Name(),
				c.cfg.Service)
		}
		c.clusterIP = ""
	}

	return c, nil
}

// getContainerPorts converts the ports to a port list of container ports.
// Adds ports for health/readiness if necessary.
func getContainerPorts(ports []echo.Port) model.PortList {
	containerPorts := make(model.PortList, 0, len(ports))
	var healthPort *model.Port
	var readyPort *model.Port
	for _, p := range ports {
		// Add the port to the set of application ports.
		cport := &model.Port{
			Name:     p.Name,
			Protocol: p.Protocol,
			Port:     p.InstancePort,
		}
		containerPorts = append(containerPorts, cport)

		switch p.Protocol {
		case protocol.GRPC:
			continue
		case protocol.HTTP:
			if p.InstancePort == httpReadinessPort {
				readyPort = cport
			}
		default:
			if p.InstancePort == tcpHealthPort {
				healthPort = cport
			}
		}
	}

	// If we haven't added the readiness/health ports, do so now.
	if readyPort == nil {
		containerPorts = append(containerPorts, &model.Port{
			Name:     "http-readiness-port",
			Protocol: protocol.HTTP,
			Port:     httpReadinessPort,
		})
	}
	if healthPort == nil {
		containerPorts = append(containerPorts, &model.Port{
			Name:     "tcp-health-port",
			Protocol: protocol.HTTP,
			Port:     tcpHealthPort,
		})
	}
	return containerPorts
}

func (c *instance) ID() resource.ID {
	return c.id
}

func (c *instance) Address() string {
	return c.clusterIP
}

func (c *instance) Workloads() ([]echo.Workload, error) {
	out := make([]echo.Workload, 0, len(c.workloads))
	for _, w := range c.workloads {
		out = append(out, w)
	}
	return out, nil
}

func (c *instance) WorkloadsOrFail(t test.Failer) []echo.Workload {
	t.Helper()
	out, err := c.Workloads()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (c *instance) WaitUntilCallable(instances ...echo.Instance) error {
	// Wait for the outbound config to be received by each workload from Pilot.
	for _, w := range c.workloads {
		if w.sidecar != nil {
			if err := w.sidecar.WaitForConfig(common.OutboundConfigAcceptFunc(instances...)); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *instance) WaitUntilCallableOrFail(t test.Failer, instances ...echo.Instance) {
	t.Helper()
	if err := c.WaitUntilCallable(instances...); err != nil {
		t.Fatal(err)
	}
}

func (c *instance) initialize(endpoints *kubeCore.Endpoints) error {
	if c.workloads != nil {
		// Already ready.
		return nil
	}

	workloads := make([]*workload, 0)
	for _, subset := range endpoints.Subsets {
		for _, addr := range subset.Addresses {
			workload, err := newWorkload(addr, c.cfg.Annotations, c.grpcPort, c.env.Accessor, c.ctx)
			if err != nil {
				return err
			}
			workloads = append(workloads, workload)
		}
	}

	if len(workloads) == 0 {
		return fmt.Errorf("no pods found for service %s/%s/%s", c.cfg.Namespace.Name(), c.cfg.Service, c.cfg.Version)
	}

	c.workloads = workloads
	return nil
}

func (c *instance) Close() (err error) {
	for _, w := range c.workloads {
		err = multierror.Append(err, w.Close()).ErrorOrNil()
	}
	c.workloads = nil
	return
}

func (c *instance) Config() echo.Config {
	return c.cfg
}

func (c *instance) Call(opts echo.CallOptions) (appEcho.ParsedResponses, error) {
	out, err := common.CallEcho(c.workloads[0].Instance, &opts, common.IdentityOutboundPortSelector)
	if err != nil {
		if opts.Port != nil {
			err = fmt.Errorf("failed calling %s->'%s://%s:%d/%s': %v",
				c.Config().Service,
				strings.ToLower(string(opts.Port.Protocol)),
				opts.Target.Config().Service,
				opts.Port.ServicePort,
				opts.Path,
				err)
		}
		return nil, err
	}
	return out, nil
}

func (c *instance) CallOrFail(t test.Failer, opts echo.CallOptions) appEcho.ParsedResponses {
	t.Helper()
	r, err := c.Call(opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
