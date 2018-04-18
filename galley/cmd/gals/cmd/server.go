// Copyright 2018 Istio Authors
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

package cmd

import (
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/galley/cmd/shared"
	"istio.io/istio/galley/pkg/kube"
	"istio.io/istio/galley/pkg/kube/sync"
	"istio.io/istio/pkg/cmd"
)

func serverCmd(fatalf shared.FormatFn) *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: "Starts Galley as a server",
		Run: func(cmd *cobra.Command, args []string) {
			err := runServer(fatalf)
			if err != nil {
				fatalf("Error during startup: %v", err)
			}
		},
	}
}

func runServer(fatalf shared.FormatFn) error {
	config, err := clientcmd.BuildConfigFromFlags("", flags.kubeConfig)
	if err != nil {
		fatalf("Error getting service config: %v", err)
		return err
	}

	kube := kube.NewKube(config)
	s := sync.New(kube, sync.Mapping(), flags.resyncPeriod)

	stop := make(chan struct{})

	s.Start()

	cmd.WaitSignal(stop)
	s.Stop()
	return nil
}
