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

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
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

	if err := initializeInstances(instances); err != nil {
		return nil, fmt.Errorf("initialize instances: %v", err)
	}
	scopes.Framework.Debugf("initialized kube echo deployments in %v", time.Since(t0))

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

func initializeInstances(instances echo.Instances) error {
	// Wait to receive the k8s Endpoints for each Echo Instance.
	wg := sync.WaitGroup{}
	aggregateErrMux := &sync.Mutex{}
	var aggregateErr error
	for _, i := range instances {
		inst := i.(*instance)
		wg.Add(1)

		serviceName := inst.Config().Service
		serviceNamespace := inst.Config().Namespace.Name()
		timeout := inst.Config().ReadinessTimeout
		cluster := inst.cluster

		// Run the waits in parallel.
		go func() {
			defer wg.Done()
			selector := "app"
			if inst.Config().DeployAsVM {
				selector = constants.TestVMLabel
			}
			// Wait until all the pods are ready for this service
			fetch := kube.NewPodMustFetch(cluster, serviceNamespace, fmt.Sprintf("%s=%s", selector, serviceName))
			pods, err := kube.WaitUntilPodsAreReady(fetch, retry.Timeout(timeout))
			if err != nil {
				aggregateErrMux.Lock()
				aggregateErr = multierror.Append(aggregateErr, err)
				aggregateErrMux.Unlock()
				return
			}
			if err := inst.initialize(pods); err != nil {
				aggregateErrMux.Lock()
				aggregateErr = multierror.Append(aggregateErr, fmt.Errorf("initialize %v/%v/%v: %v", inst.ID(), inst.Config().Service, inst.Address(), err))
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
