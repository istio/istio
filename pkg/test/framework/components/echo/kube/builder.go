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

package kube

import (
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

func init() {
	echo.RegisterFactory(cluster.Kubernetes, build)
}

func build(ctx resource.Context, configs []echo.Config) (echo.Instances, error) {
	t0 := time.Now()
	instances, err := newInstances(ctx, configs)
	if err != nil {
		return nil, fmt.Errorf("build instance: %v", err)
	}
	scopes.Framework.Debugf("created echo deployments in %v", time.Since(t0))

	if err := startAll(instances); err != nil {
		return nil, fmt.Errorf("failed starting kube echo instances: %v", err)
	}
	scopes.Framework.Debugf("successfully started kube echo instances in %v", time.Since(t0))

	return instances, nil
}

func newInstances(ctx resource.Context, configs []echo.Config) (echo.Instances, error) {
	// TODO consider making this parallel. This was attempted but had issues with concurrent writes
	// it should be possible though.
	instances := make([]echo.Instance, 0, len(configs))
	for _, cfg := range configs {
		inst, err := newInstance(ctx, cfg)
		if err != nil {
			return nil, err
		}
		instances = append(instances, inst)
	}
	return instances, nil
}

func startAll(instances echo.Instances) error {
	// Wait to receive the k8s Endpoints for each Echo Instance.
	wg := sync.WaitGroup{}
	aggregateErrMux := &sync.Mutex{}
	var aggregateErr error
	for _, i := range instances {
		inst := i.(*instance)
		wg.Add(1)

		// Run the waits in parallel.
		go func() {
			defer wg.Done()

			if err := inst.Start(); err != nil {
				aggregateErrMux.Lock()
				aggregateErr = multierror.Append(aggregateErr, fmt.Errorf("start %v/%v/%v: %v",
					inst.ID(), inst.Config().Service, inst.Address(), err))
				aggregateErrMux.Unlock()
			}
		}()
	}

	wg.Wait()

	if aggregateErr != nil {
		return aggregateErr
	}

	return nil
}
