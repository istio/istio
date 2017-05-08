// Copyright 2017 Istio Authors
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
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"istio.io/manager/client/proxy"
	"istio.io/manager/platform/kube"

	"github.com/ghodss/yaml"
	rpc "github.com/googleapis/googleapis/google/rpc"
	"github.com/spf13/cobra"
)

// TODO This should come from something like istio.io/api instead of
// being hand copied from istio.io/mixer.
type mixerAPIResponse struct {
	Data   interface{} `json:"data,omitempty"`
	Status rpc.Status  `json:"status,omitempty"`
}

const (
	requestTimeout = 60 * time.Second
)

var (
	mixerFile            string
	mixerAPIServerAddr   string // deprecated
	istioMixerAPIService string
	mixerRESTRequester   proxy.RESTRequester

	mixerCmd = &cobra.Command{
		Use:   "mixer",
		Short: "Istio Mixer configuration",
		Long: `
The Mixer configuration API allows users to configure all facets of the
Mixer.

See https://istio.io/docs/concepts/policy-and-control/mixer-config.html
for a description of Mixer configuration's scope, subject, and rules.
`,
		SilenceUsage: true,
		PersistentPreRunE: func(c *cobra.Command, args []string) error {
			var err error
			client, err = kubeClientFromConfig(kubeconfig)
			if err != nil {
				return err
			}

			if useKubeRequester {
				// TODO temporarily use namespace instead of
				// istioNamespace until istio/istio e2e tests are
				// updated.
				if istioNamespace == "" {
					istioNamespace = namespace
				}
				mixerRESTRequester = &k8sRESTRequester{
					client:    client,
					namespace: istioNamespace,
					service:   istioMixerAPIService,
				}
			} else {
				mixerRESTRequester = &proxy.BasicHTTPRequester{
					BaseURL: istioMixerAPIService,
					Client:  &http.Client{Timeout: requestTimeout},
					Version: kube.IstioResourceVersion,
				}
			}
			return nil
		},
	}

	mixerRuleCmd = &cobra.Command{
		Use:   "rule",
		Short: "Istio Mixer Rule configuration",
		Long: `
Create and list Mixer rules in the configuration server.
`,
		SilenceUsage: true,
	}

	mixerRuleCreateCmd = &cobra.Command{
		Use:   "create",
		Short: "Create Istio Mixer rules",
		Long: `
Example usage:

    # Create a new Mixer rule for the given scope and subject.
    istioctl mixer rule create global myservice.ns.svc.cluster.local -f mixer-rule.yml
`,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 2 || mixerFile == "" {
				return errors.New(c.UsageString())
			}
			rule, err := ioutil.ReadFile(mixerFile)
			if err != nil {
				return fmt.Errorf("failed opening %s: %v", mixerFile, err)
			}
			return mixerRuleCreate(args[0], args[1], rule)
		},
	}
	mixerRuleGetCmd = &cobra.Command{
		Use:   "get",
		Short: "Get Istio Mixer rules",
		Long: `
Get a Mixer rule for a given scope and subject.

Example usage:

	# Get the Mixer rule with scope='global' and subject='myservice.ns.svc.cluster.local'
    istioctl mixer rule get global myservice.ns.svc.cluster.local
`,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 2 {
				return errors.New(c.UsageString())
			}
			out, err := mixerRuleGet(args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Println(out)
			return nil
		},
	}
)

func mixerRulePath(scope, subject string) string {
	return fmt.Sprintf("api/v1/scopes/%s/subjects/%s/rules", scope, subject)
}

func mixerRuleCreate(scope, subject string, rule []byte) error {
	path := mixerRulePath(scope, subject)
	status, body, err := mixerRESTRequester.Request(http.MethodPut, path, rule)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		var response mixerAPIResponse
		message := "unknown"
		if err = json.Unmarshal(body, &response); err == nil {
			message = response.Status.Message
		}
		return fmt.Errorf("failed rule creation with status %q: %q", status, message)
	}
	return err
}

func mixerRuleGet(scope, subject string) (string, error) {
	path := mixerRulePath(scope, subject)
	status, body, err := mixerRESTRequester.Request(http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", errors.New(http.StatusText(status))
	}

	var response mixerAPIResponse
	if err = json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed processing response: %v", err)
	}
	data, err := yaml.Marshal(response.Data)
	if err != nil {
		return "", fmt.Errorf("failed formatting response: %v", err)
	}
	return string(data), nil
}

func init() {
	mixerRuleCreateCmd.PersistentFlags().StringVarP(&mixerFile, "file", "f", "",
		"Input file with contents of the Mixer rule")
	mixerCmd.PersistentFlags().StringVar(&istioMixerAPIService,
		"mixerAPIService", "istio-mixer:9094",
		"Name of istio-mixer service. When --kube=false this sets the address of the mixer service")
	// TODO remove this flag once istio/istio integration tests are
	// updated to use mixer service
	mixerCmd.PersistentFlags().StringVar(&mixerAPIServerAddr, "mixer", os.Getenv("ISTIO_MIXER_API_SERVER"),
		"(deprecated) Address of the Mixer configuration server as <host>:<port>")

	mixerRuleCmd.AddCommand(mixerRuleCreateCmd)
	mixerRuleCmd.AddCommand(mixerRuleGetCmd)
	mixerCmd.AddCommand(mixerRuleCmd)
	rootCmd.AddCommand(mixerCmd)
}
