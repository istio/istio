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
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"istio.io/istio/pilot/cmd/pilot-agent/status/ready"
	"istio.io/pkg/log"
)

var (
	timeoutSeconds       int
	requestTimeoutMillis int
	periodMillis         int
	url                  string
	uds                  string

	waitCmd = &cobra.Command{
		Use:   "wait",
		Short: "Waits until the Envoy proxy is ready",
		RunE: func(c *cobra.Command, args []string) error {
			probe := ready.NewStatusEnvoyListenerProbe(url, uds, time.Duration(requestTimeoutMillis)*time.Millisecond)
			log.Infof("Waiting for Envoy proxy to be ready (timeout: %d seconds)...", timeoutSeconds)

			var err error
			timeoutAt := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
			for time.Now().Before(timeoutAt) {
				err = probe.Check()
				if err == nil {
					log.Infof("Envoy is ready!")
					return nil
				}
				log.Debugf("Not ready yet: %v", err)
				time.Sleep(time.Duration(periodMillis) * time.Millisecond)
			}
			return fmt.Errorf("timeout waiting for Envoy proxy to become ready. Last error: %v", err)
		},
	}
)

func init() {
	waitCmd.PersistentFlags().IntVar(&timeoutSeconds, "timeoutSeconds", 60, "maximum number of seconds to wait for Envoy to be ready")
	waitCmd.PersistentFlags().IntVar(&requestTimeoutMillis, "requestTimeoutMillis", 500, "number of milliseconds to wait for response")
	waitCmd.PersistentFlags().IntVar(&periodMillis, "periodMillis", 500, "number of milliseconds to wait between attempts")
	waitCmd.PersistentFlags().StringVar(&url, "url", ready.DefaultStatusEnvoyListenerURL, "URL to use in requests")
	waitCmd.PersistentFlags().StringVar(&uds, "uds", ready.DefaultStatusEnvoyListenerUDS, "UDS socket used in requests")

	rootCmd.AddCommand(waitCmd)
}
