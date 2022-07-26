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

package staticvm

import (
	"fmt"
	"net"
	"strings"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test"
	echoClient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
)

var _ echo.Workload = &workload{}

type workload struct {
	*echoClient.Client
	cluster cluster.Cluster
	address string
}

func newWorkloads(address []string, grpcPort int, tls *common.TLSSettings, c cluster.Cluster) (echo.Workloads, error) {
	var errs error
	var out echo.Workloads
	for _, ip := range address {
		w, err := newWorkload(ip, grpcPort, tls, c)
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

func newWorkload(addresses string, grpcPort int, tls *common.TLSSettings, cl cluster.Cluster) (*workload, error) {
	var (
		external string
		internal string
	)
	parts := strings.Split(addresses, ":")
	external = parts[0]
	if len(parts) > 1 {
		internal = parts[1]
	}

	c, err := echoClient.New(net.JoinHostPort(external, fmt.Sprint(grpcPort)), tls)
	if err != nil {
		return nil, err
	}
	return &workload{
		Client:  c,
		cluster: cl,
		address: internal,
	}, nil
}

func (w *workload) PodName() string {
	return ""
}

func (w *workload) Address() string {
	return w.address
}

func (w *workload) Cluster() cluster.Cluster {
	return w.cluster
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
