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
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"istio.io/pkg/log"
)

var (
	timeoutSeconds       int
	requestTimeoutMillis int
	periodMillis         int
	url                  string

	waitCmd = &cobra.Command{
		Use:   "wait",
		Short: "Waits until the Envoy proxy is ready",
		RunE: func(c *cobra.Command, args []string) error {
			client := &http.Client{
				Timeout: time.Duration(requestTimeoutMillis) * time.Millisecond,
			}
			log.Infof("Waiting for Envoy proxy to be ready (timeout: %d seconds)...", timeoutSeconds)

			var err error
			timeoutAt := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
			for time.Now().Before(timeoutAt) {
				err = checkIfReady(client, url)
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

func checkIfReady(client *http.Client, url string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP status code %v", resp.StatusCode)
	}
	return nil
}

func init() {
	waitCmd.PersistentFlags().IntVar(&timeoutSeconds, "timeoutSeconds", 60, "maximum number of seconds to wait for Envoy to be ready")
	waitCmd.PersistentFlags().IntVar(&requestTimeoutMillis, "requestTimeoutMillis", 500, "number of milliseconds to wait for response")
	waitCmd.PersistentFlags().IntVar(&periodMillis, "periodMillis", 500, "number of milliseconds to wait between attempts")
	waitCmd.PersistentFlags().StringVar(&url, "url", "http://localhost:15021/healthz/ready", "URL to use in requests")

	rootCmd.AddCommand(waitCmd)
}
