// Copyright 2018 The Operator-SDK Authors
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

package operator

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/operator-framework/operator-sdk/pkg/ansible/controller"
	"github.com/operator-framework/operator-sdk/pkg/ansible/flags"
	"github.com/operator-framework/operator-sdk/pkg/ansible/proxy/controllermap"
	"github.com/operator-framework/operator-sdk/pkg/ansible/runner"

	"sigs.k8s.io/controller-runtime/pkg/manager"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/runtime/signals"
)

// Run - A blocking function which starts a controller-runtime manager
// It starts an Operator by reading in the values in `./watches.yaml`, adds a controller
// to the manager, and finally running the manager.
func Run(done chan error, mgr manager.Manager, f *flags.AnsibleOperatorFlags, cMap *controllermap.ControllerMap) {
	watches, err := runner.NewFromWatches(f.WatchesFile)
	if err != nil {
		logf.Log.WithName("manager").Error(err, "Failed to get watches")
		done <- err
		return
	}
	rand.Seed(time.Now().Unix())
	c := signals.SetupSignalHandler()

	for gvk, runner := range watches {

		// if the WORKER_* environment variable is set, use that value.
		// Otherwise, use the value from the CLI. This is definitely
		// counter-intuitive but it allows the operator admin adjust the
		// number of workers based on their cluster resources. While the
		// author may use the CLI option to specify a suggested
		// configuration for the operator.
		maxWorkers := getMaxWorkers(gvk, f.MaxWorkers)

		o := controller.Options{
			GVK:          gvk,
			Runner:       runner,
			ManageStatus: runner.GetManageStatus(),
			MaxWorkers:   maxWorkers,
		}
		applyFlagsToControllerOptions(f, &o)
		if d, ok := runner.GetReconcilePeriod(); ok {
			o.ReconcilePeriod = d
		}
		ctr := controller.Add(mgr, o)
		if ctr == nil {
			done <- errors.New("failed to add controller")
			return
		}
		cMap.Store(o.GVK, &controllermap.Contents{Controller: *ctr,
			WatchDependentResources:     runner.GetWatchDependentResources(),
			WatchClusterScopedResources: runner.GetWatchClusterScopedResources(),
			OwnerWatchMap:               controllermap.NewWatchMap(),
			AnnotationWatchMap:          controllermap.NewWatchMap(),
		})
	}
	done <- mgr.Start(c)
}

func getMaxWorkers(gvk schema.GroupVersionKind, defvalue int) int {
	envvar := formatEnvVar(gvk.Kind, gvk.Group)
	maxWorkers, err := strconv.Atoi(os.Getenv(envvar))
	if err != nil {
		// we don't care why we couldn't parse it just use one.
		// maybe we should log that we are defaulting to 1.
		logf.Log.WithName("manager").V(0).Info(fmt.Sprintf("Using default value for workers %d", defvalue))
		return defvalue
	}

	return maxWorkers
}

func formatEnvVar(kind string, group string) string {
	envvar := fmt.Sprintf("WORKER_%s_%s", kind, group)
	return strings.ToUpper(strings.Replace(envvar, ".", "_", -1))
}

func applyFlagsToControllerOptions(f *flags.AnsibleOperatorFlags, o *controller.Options) {
	o.ReconcilePeriod = f.ReconcilePeriod
}
