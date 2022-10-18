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

package main

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/tests/util/leak"
)

func executeCommand(root *cobra.Command, args ...string) (output string, err error) {
	_, output, err = executeCommandC(root, args...)
	return output, err
}

func executeCommandC(root *cobra.Command, args ...string) (c *cobra.Command, output string, err error) {
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)

	c, err = root.ExecuteC()

	return c, buf.String(), err
}

func TestAddServerFlags(t *testing.T) {
	tests := []struct {
		desc                    string
		force                   bool
		maxConcurrentReconciles int
	}{
		{
			desc:                    "no concurrent reconcile",
			force:                   false,
			maxConcurrentReconciles: 1,
		},
		{
			desc:                    "enable concurrent reconcile",
			force:                   false,
			maxConcurrentReconciles: 100,
		},
		{
			desc:                    "enable concurrent reconcile and force to proceed",
			force:                   true,
			maxConcurrentReconciles: 1000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			originForce := fmt.Sprintf("%+v", tt.force)
			originMCR := fmt.Sprintf("%+v", tt.maxConcurrentReconciles)

			cmd := serverCmd(func(sArgs *serverArgs) {
				watchChan := make(chan event.GenericEvent, 1)
				watch := &source.Channel{Source: watchChan}
				watchChan <- event.GenericEvent{Object: &corev1.Pod{}}

				var wg sync.WaitGroup
				wg.Add(1)

				reconcileStarted := make(chan struct{})
				controllerFinished := make(chan struct{})
				rec := reconcile.Func(func(context.Context, reconcile.Request) (reconcile.Result, error) {
					close(reconcileStarted)
					wg.Wait()
					return reconcile.Result{}, nil
				})
				//cfg := kube.NewClientConfigForRestConfig()
				cfg, _ := config.GetConfig()

				//var testenv *envtest.Environment
				//testenv = &envtest.Environment{}
				//cfg, _ := testenv.Start()

				m, _ := manager.New(cfg, manager.Options{})

				c, _ := controller.New("new-controller", m, controller.Options{Reconciler: rec, MaxConcurrentReconciles: sArgs.maxConcurrentReconciles})
				c.Watch(watch, &handler.EnqueueRequestForObject{})

				ctx, cancel := context.WithCancel(context.Background())
				go func() {
					m.Start(ctx)
					close(controllerFinished)
				}()

				<-reconcileStarted
				goRoutineNum, _ := leak.InterestingGoroutines()
				actualMCR := fmt.Sprintf("%d", sArgs.maxConcurrentReconciles)
				actualNum := fmt.Sprintf("%+v", len(goRoutineNum))
				assert.Equal(t, true, actualNum > actualMCR)
				wg.Done()
				cancel()
				<-controllerFinished
			})

			executeCommand(cmd, "--max-concurrent-reconciles="+originMCR, "--force="+originForce)
		})
	}
}
