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

package asmvm

import (
	"fmt"

	"github.com/hashicorp/go-multierror"
	"google.golang.org/api/compute/v1"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/scopes"
)

var _ echo.Workload = &workload{}

type workload struct {
	*client.Instance
	address string
}

func newWorkloads(migInstances []*compute.Instance, grpcPort int, tls *common.TLSSettings) ([]echo.Workload, error) {
	var out []echo.Workload
	var errs error
	for _, i := range migInstances {
		if len(i.NetworkInterfaces) < 1 || len(i.NetworkInterfaces[0].AccessConfigs) < 1 {
			return nil, fmt.Errorf("cannot determine ips for %s via networkInterfaces[].accessConfigs", i.Name)
		}
		externalIP := i.NetworkInterfaces[0].AccessConfigs[0].NatIP
		internalIP := i.NetworkInterfaces[0].NetworkIP
		scopes.Framework.Infof("%s:\n  external IP: %s\n  internal IP: %s\n  status: %s",
			i.Name, externalIP, internalIP, i.Status)

		w, err := newWorkload(externalIP, internalIP, grpcPort, tls)
		if err != nil {
			errs = multierror.Append(errs, err)
		}
		out = append(out, w)

	}

	if errs != nil {
		return nil, errs
	}
	return out, nil
}

func newWorkload(grpcAddr, internalAddr string, grpcPort int, tls *common.TLSSettings) (*workload, error) {
	c, err := client.New(fmt.Sprintf("%s:%d", grpcAddr, grpcPort), tls)
	if err != nil {
		return nil, err
	}
	return &workload{
		Instance: c,
		address:  internalAddr,
	}, nil
}

func (w *workload) PodName() string {
	return ""
}

func (w *workload) Address() string {
	return w.address
}

func (w *workload) Sidecar() echo.Sidecar {
	panic("implement me")
}

func (w *workload) Logs() (string, error) {
	panic("implement me")
}

func (w *workload) LogsOrFail(_ test.Failer) string {
	panic("implement me")
}
