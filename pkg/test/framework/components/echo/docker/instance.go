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

package docker

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	appEcho "istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/framework/components/environment/native"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/pkg/log"
)

const (
	noSidecarWaitDuration = 10 * time.Second
)

var (
	_ echo.Instance   = &instance{}
	_ io.Closer       = &instance{}
	_ resource.Dumper = &instance{}
)

type instance struct {
	id        resource.ID
	cfg       echo.Config
	se        *serviceEntry
	ctx       resource.Context
	workloads []*workload
}

func newInstance(ctx resource.Context, cfg echo.Config) (out *instance, err error) {
	e := ctx.Environment().(*native.Environment)

	// Fill in defaults for any missing values.
	common.AddPortIfMissing(&cfg, protocol.GRPC)
	common.AddPortIfMissing(&cfg, protocol.HTTP)
	if err = common.FillInDefaults(ctx, e.Domain, &cfg); err != nil {
		return nil, err
	}

	if !cfg.Headless {
		log.Debugf("Forcing Headless=true for Echo instance %s since "+
			"ClusterIPs are not supported in the native environment. If using TCP ports,"+
			"this may result in a harmless ADS conflict due to duplicate outbound listeners",
			cfg.FQDN())
		cfg.Headless = true
	}

	if cfg.ServiceAccount {
		log.Debugf("Forcing ServiceAccounts=false for Echo instance %s since "+
			"service accounts are not supported by the native environment.",
			cfg.FQDN())
		cfg.ServiceAccount = false
	}

	var dumpDir string
	dumpDir, err = ctx.CreateTmpDirectory(cfg.FQDN())
	if err != nil {
		scopes.CI.Errorf("Unable to create output directory for %s: %v", cfg.FQDN(), err)
		return
	}

	i := &instance{
		cfg: cfg,
		ctx: ctx,
	}
	i.id = ctx.TrackResource(i)

	defer func() {
		if err != nil {
			i.Dump()
			_ = i.Close()
		}
	}()

	// Validate the configuration.
	if cfg.Galley == nil {
		// Galley is not actually required currently, but it will be once Pilot gets
		// all resources from Galley. Requiring now for forward-compatibility.
		return nil, errors.New("galley must be provided")
	}
	if cfg.Pilot == nil {
		return nil, errors.New("pilot must be provided")
	}

	w, err := newWorkload(e, cfg, dumpDir)
	if err != nil {
		return nil, err
	}
	i.workloads = append(i.workloads, w)

	// Apply the configuration for the service to Galley.
	i.se, err = newServiceEntry(cfg.Galley, w.Address(), cfg)
	if err != nil {
		return nil, err
	}

	// Wait until the pilot agent and the echo application are ready to receive traffic.
	if err = w.waitForReady(); err != nil {
		return nil, err
	}

	return i, nil
}

func (i *instance) ID() resource.ID {
	return i.id
}

func (i *instance) Address() string {
	return i.workloads[0].Address()
}

func (i *instance) Workloads() ([]echo.Workload, error) {
	out := make([]echo.Workload, 0, len(i.workloads))
	for _, w := range i.workloads {
		out = append(out, w)
	}
	return out, nil
}

func (i *instance) WorkloadsOrFail(t test.Failer) []echo.Workload {
	t.Helper()
	out, err := i.Workloads()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (i *instance) WaitUntilCallable(instances ...echo.Instance) error {
	// Wait for the outbound config to be received by each workload from Pilot.
	for _, w := range i.workloads {
		if w.sidecar != nil {
			if err := w.sidecar.WaitForConfig(common.OutboundConfigAcceptFunc(instances...)); err != nil {
				return err
			}
		}
	}

	if !i.cfg.Annotations.GetBool(echo.SidecarInject) {
		time.Sleep(noSidecarWaitDuration)
	}

	return nil
}

func (i *instance) WaitUntilCallableOrFail(t test.Failer, instances ...echo.Instance) {
	t.Helper()
	if err := i.WaitUntilCallable(instances...); err != nil {
		t.Fatal(err)
	}
}

func (i *instance) Close() (err error) {
	if i.se != nil {
		err = multierror.Append(err, i.se.Close()).ErrorOrNil()
	}
	for _, w := range i.workloads {
		err = multierror.Append(err, w.Close()).ErrorOrNil()
	}
	i.workloads = nil
	return
}

func (i *instance) Config() echo.Config {
	return i.cfg
}

func (i *instance) Call(opts echo.CallOptions) (appEcho.ParsedResponses, error) {
	out, err := common.CallEcho(i.workloads[0].Instance, &opts, common.IdentityOutboundPortSelector)
	if err != nil {
		if opts.Port != nil {
			err = fmt.Errorf("failed calling %s->'%s://%s:%d/%s': %v",
				i.Config().Service,
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

func (i *instance) CallOrFail(t test.Failer, opts echo.CallOptions) appEcho.ParsedResponses {
	t.Helper()
	r, err := i.Call(opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (i *instance) Dump() {
	scopes.CI.Errorf("=== Dumping state for Echo %s..,", i.cfg.FQDN())

	for _, w := range i.workloads {
		if w != nil {
			w.Dump()
		}
	}
}
