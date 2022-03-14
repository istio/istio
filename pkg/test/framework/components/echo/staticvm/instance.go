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
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/hashicorp/go-multierror"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	echoClient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/framework/resource"
)

var _ echo.Instance = &instance{}

func init() {
	echo.RegisterFactory(cluster.StaticVM, newInstances)
}

type instance struct {
	id        resource.ID
	config    echo.Config
	address   string
	workloads echo.Workloads
}

func newInstances(ctx resource.Context, config []echo.Config) (echo.Instances, error) {
	errG := multierror.Group{}
	mu := sync.Mutex{}
	var out echo.Instances
	for _, c := range config {
		c := c
		errG.Go(func() error {
			i, err := newInstance(ctx, c)
			if err != nil {
				return err
			}
			mu.Lock()
			defer mu.Unlock()
			out = append(out, i)
			return nil
		})
	}
	if err := errG.Wait().ErrorOrNil(); err != nil {
		return nil, err
	}
	return out, nil
}

func newInstance(ctx resource.Context, config echo.Config) (echo.Instance, error) {
	// TODO is there a need for static cluster to create workload group/entry?

	grpcPort, found := config.Ports.ForProtocol(protocol.GRPC)
	if !found {
		return nil, errors.New("unable fo find GRPC command port")
	}
	workloads, err := newWorkloads(config.StaticAddresses, grpcPort.WorkloadPort, config.TLSSettings, config.Cluster)
	if err != nil {
		return nil, err
	}
	if len(workloads) == 0 {
		return nil, fmt.Errorf("no workloads for %s", config.Service)
	}
	svcAddr, err := getClusterIP(config)
	if err != nil {
		return nil, err
	}
	i := &instance{
		config:    config,
		address:   svcAddr,
		workloads: workloads,
	}
	i.id = ctx.TrackResource(i)
	return i, nil
}

func getClusterIP(config echo.Config) (string, error) {
	svc, err := config.Cluster.Primary().CoreV1().
		Services(config.Namespace.Name()).Get(context.TODO(), config.Service, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return svc.Spec.ClusterIP, nil
}

func (i *instance) ID() resource.ID {
	return i.id
}

func (i *instance) NamespacedName() model.NamespacedName {
	return i.config.NamespacedName()
}

func (i *instance) PortForName(name string) echo.Port {
	return i.Config().Ports.MustForName(name)
}

func (i *instance) Config() echo.Config {
	return i.config
}

func (i *instance) Address() string {
	return i.address
}

func (i *instance) Addresses() []string {
	return []string{i.address}
}

func (i *instance) Workloads() (echo.Workloads, error) {
	return i.workloads, nil
}

func (i *instance) WorkloadsOrFail(t test.Failer) echo.Workloads {
	w, err := i.Workloads()
	if err != nil {
		t.Fatalf("failed getting workloads for %s", i.Config().Service)
	}
	return w
}

func (i *instance) MustWorkloads() echo.Workloads {
	out, err := i.Workloads()
	if err != nil {
		panic(err)
	}
	return out
}

func (i *instance) Clusters() cluster.Clusters {
	var out cluster.Clusters
	if i.config.Cluster != nil {
		out = append(out, i.config.Cluster)
	}
	return out
}

func (i *instance) Instances() echo.Instances {
	return echo.Instances{i}
}

func (i *instance) defaultClient() (*echoClient.Client, error) {
	return i.workloads[0].(*workload).Client, nil
}

func (i *instance) Call(opts echo.CallOptions) (echoClient.Responses, error) {
	return common.ForwardEcho(i.Config().Service, i.defaultClient, &opts)
}

func (i *instance) CallOrFail(t test.Failer, opts echo.CallOptions) echoClient.Responses {
	t.Helper()
	res, err := i.Call(opts)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (i *instance) Restart() error {
	panic("cannot trigger restart of a static VM")
}
