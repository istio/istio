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

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/framework/components/echo"
)

var _ echo.Workload = &workload{}

type workload struct {
	*client.Instance
	address string
}

func newWorkloads(address []string, grpcPort int, tls *common.TLSSettings) ([]echo.Workload, error) {
	var errs error
	var out []echo.Workload
	for _, ip := range address {
		w, err := newWorkload(ip, grpcPort, tls)
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

func newWorkload(address string, grpcPort int, tls *common.TLSSettings) (*workload, error) {
	c, err := client.New(fmt.Sprintf("%s:%d", address, grpcPort), tls)
	if err != nil {
		return nil, err
	}
	return &workload{
		Instance: c,
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
