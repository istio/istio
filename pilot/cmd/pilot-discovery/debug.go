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
	"os"

	"github.com/spf13/cobra"

	"istio.io/istio/pkg/log"
)

const all = "all"

type debug struct {
	pilotAddress string
	client       *http.Client
}

var (
	configTypes = map[string]string{
		"all": "",
		"ads": "adsz",
		"eds": "edsz",
	}

	debugCmd = &cobra.Command{
		Use:   "debug <proxyID> <configuration-type>",
		Short: "Retrieve the configuration for the specified proxy",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			d := &debug{
				pilotAddress: "127.0.0.1:9093",
				client:       &http.Client{},
			}
			return d.run(args)
		},
	}
)

func (d *debug) run(args []string) error {
	proxyID := args[0]
	configType := args[1]
	if err := validateConfigType(configType); err != nil {
		return err
	}

	if configType == all {
		for ct := range configTypes {
			if ct != all {
				if err := d.printConfig(ct, proxyID); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return d.printConfig(configType, proxyID)
}

func (d *debug) printConfig(typ, proxyID string) error {
	req, err := http.NewRequest("GET", fmt.Sprintf("http://%v/debug/%s", d.pilotAddress, configTypes[typ]), nil)
	if err != nil {
		return err
	}
	if proxyID != all {
		q := req.URL.Query()
		q.Add("proxyID", proxyID)
		req.URL.RawQuery = q.Encode()
	}
	resp, err := d.client.Do(req)
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
	} else if resp.StatusCode == 404 {
		fmt.Fprintf(os.Stderr, "proxy not connected to this Pilot instance")
	} else {
		return fmt.Errorf("received %v status from Pilot: %v", resp.StatusCode, string(bytes))
	}
	return nil
}

func validateConfigType(typ string) error {
	if _, ok := configTypes[typ]; !ok {
		return fmt.Errorf("%q is not a supported debugging config type", typ)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(debugCmd)
}
