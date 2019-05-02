// Copyright 2019 The Operator-SDK Authors
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

package helm

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/operator-framework/operator-sdk/pkg/helm/controller"
	hoflags "github.com/operator-framework/operator-sdk/pkg/helm/flags"
	"github.com/operator-framework/operator-sdk/pkg/helm/release"
	"github.com/operator-framework/operator-sdk/pkg/helm/watches"
	"github.com/operator-framework/operator-sdk/pkg/k8sutil"
	"github.com/operator-framework/operator-sdk/pkg/leader"
	sdkVersion "github.com/operator-framework/operator-sdk/version"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/runtime/signals"
)

var log = logf.Log.WithName("cmd")

func printVersion() {
	log.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	log.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
	log.Info(fmt.Sprintf("Version of operator-sdk: %v", sdkVersion.Version))
}

// Run runs the helm operator
func Run(flags *hoflags.HelmOperatorFlags) error {
	printVersion()

	namespace, found := os.LookupEnv(k8sutil.WatchNamespaceEnvVar)
	log = log.WithValues("Namespace", namespace)
	if found {
		if namespace == metav1.NamespaceAll {
			log.Info("Watching all namespaces.")
		} else {
			log.Info("Watching single namespace.")
		}
	} else {
		log.Info(fmt.Sprintf("%v environment variable not set. Watching all namespaces.",
			k8sutil.WatchNamespaceEnvVar))
		namespace = metav1.NamespaceAll
	}

	cfg, err := config.GetConfig()
	if err != nil {
		log.Error(err, "Failed to get config.")
		return err
	}
	mgr, err := manager.New(cfg, manager.Options{
		Namespace: namespace,
	})
	if err != nil {
		log.Error(err, "Failed to create a new manager.")
		return err
	}

	watches, err := watches.Load(flags.WatchesFile)
	if err != nil {
		log.Error(err, "Failed to create new manager factories.")
		return err
	}

	for _, w := range watches {
		// Register the controller with the factory.
		err := controller.Add(mgr, controller.WatchOptions{
			Namespace:               namespace,
			GVK:                     w.GroupVersionKind,
			ManagerFactory:          release.NewManagerFactory(mgr, w.ChartDir),
			ReconcilePeriod:         flags.ReconcilePeriod,
			WatchDependentResources: w.WatchDependentResources,
		})
		if err != nil {
			log.Error(err, "Failed to add manager factory to controller.")
			return err
		}
	}

	operatorName, err := k8sutil.GetOperatorName()
	if err != nil {
		log.Error(err, "Failed to get operator name")
		return err
	}
	// Become the leader before proceeding
	err = leader.Become(context.TODO(), operatorName+"-lock")
	if err != nil {
		log.Error(err, "Failed to become leader.")
		return err
	}

	// Start the Cmd
	if err = mgr.Start(signals.SetupSignalHandler()); err != nil {
		log.Error(err, "Manager exited non-zero.")
		os.Exit(1)
	}
	return nil
}
