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

package main

import (
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/spf13/cobra"

	"istio.io/istio/pkg/log"
)

type status struct {
	pilotAddress string
	client       *http.Client
}

var (
	statusCmd = &cobra.Command{
		Use:   "status",
		Short: "Retrieves the status of all proxies connected to the local pilot instances",
		RunE: func(c *cobra.Command, args []string) error {
			s := &status{
				pilotAddress: "127.0.0.1:9093",
				client:       &http.Client{},
			}
			return s.run()
		},
	}
)

func (s *status) run() error {
	req, err := http.NewRequest("GET", fmt.Sprintf("http://%v/debug/syncz", s.pilotAddress), nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Errorf("Error closing response body: %v", err)
		}
	}()
	bytes, _ := ioutil.ReadAll(resp.Body)
	if resp.StatusCode == 200 {
		fmt.Println(string(bytes))
	} else {
		return fmt.Errorf("received %v status from Pilot: %v", resp.StatusCode, string(bytes))
	}
	return nil
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
