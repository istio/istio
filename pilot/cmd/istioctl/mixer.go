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
	"net/url"
	"os"

	"github.com/ghodss/yaml"
	rpc "github.com/googleapis/googleapis/google/rpc"
	"github.com/spf13/cobra"
	"k8s.io/client-go/rest"

	"istio.io/pilot/adapter/config/crd"
)

// TODO This should come from something like istio.io/api instead of
// being hand copied from istio.io/mixer.
type mixerAPIResponse struct {
	Data   interface{} `json:"data,omitempty"`
	Status rpc.Status  `json:"status,omitempty"`
}

const (
	scopesPath = "api/v1/scopes/"
)

type k8sRESTRequester struct {
	namespace string
	service   string
	client    *rest.RESTClient
}

// Request sends requests through the Kubernetes apiserver proxy to
// the a Kubernetes service.
// (see https://kubernetes.io/docs/concepts/cluster-administration/access-cluster/#discovering-builtin-services)
func (rr *k8sRESTRequester) Request(method, path string, inBody []byte) (int, []byte, error) {
	// Kubernetes apiserver proxy prefix for the specified namespace and service.
	absPath := fmt.Sprintf("api/v1/namespaces/%s/services/%s/proxy", rr.namespace, rr.service)

	// API server resource path.
	absPath += "/" + path

	var status int
	outBody, err := rr.client.Verb(method).
		AbsPath(absPath).
		SetHeader("Content-Type", "application/json").
		Body(inBody).
		Do().
		StatusCode(&status).
		Raw()
	return status, outBody, err
}

var (
	mixerFile            string
	mixerFileContent     []byte
	mixerAPIServerAddr   string // deprecated
	istioMixerAPIService string
	mixerRESTRequester   *k8sRESTRequester

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
			restconfig, err := crd.CreateRESTConfig(kubeconfig)
			if err != nil {
				return err
			}
			client, err := rest.RESTClientFor(restconfig)
			if err != nil {
				return err
			}

			mixerRESTRequester = &k8sRESTRequester{
				client:    client,
				namespace: namespace,
				service:   istioMixerAPIService,
			}

			if c.Name() == "create" {
				if mixerFile == "" {
					return errors.New(c.UsageString())
				}
				data, err := ioutil.ReadFile(mixerFile)
				if err != nil {
					return fmt.Errorf("failed opening %s: %v", mixerFile, err)
				}
				mixerFileContent = data
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
		Use:   "create <scope> <subject>",
		Short: "Create Istio Mixer rules",
		Example: `
# Create a new Mixer rule for the given scope and subject.
istioctl mixer rule create global myservice.ns.svc.cluster.local -f mixer-rule.yml
`,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 2 {
				return errors.New(c.UsageString())
			}
			return mixerRuleCreate(args[0], args[1], mixerFileContent)
		},
	}
	mixerRuleGetCmd = &cobra.Command{
		Use:   "get <scope> <subject>",
		Short: "Get Istio Mixer rules",
		Long: `
Get Mixer rules for a given scope and subject.
`,
		Example: `
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
	mixerRuleDeleteCmd = &cobra.Command{
		Use:   "delete <scope> <subject>",
		Short: "Delete Istio Mixer rules",
		Long: `
Delete Mixer rules for a given scope and subject.
`,
		Example: `
# Delete Mixer rules with scope='global' and subject='myservice.ns.svc.cluster.local'
istioctl mixer rule delete global myservice.ns.svc.cluster.local
`,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 2 {
				return errors.New(c.UsageString())
			}
			return mixerRuleDelete(args[0], args[1])
		},
	}

	mixerAdapterCmd = &cobra.Command{
		Use:          "adapter",
		Short:        "Istio Mixer Adapter configuration",
		Long:         "Create and list Mixer adapters in the configuration server.",
		SilenceUsage: true,
	}

	mixerAdapterCreateCmd = &cobra.Command{
		Use:   "create <scope>",
		Short: "Create Istio Mixer adapters",
		Example: `
# Create new Mixer adapter configs for the given scope.
istioctl mixer adapter create global -f adapters.yml
`,
		RunE: mixerAdapterOrDescriptorCreateRunE,
	}

	mixerAdapterGetCmd = &cobra.Command{
		Use:   "get <scope>",
		Short: "Get Istio Mixer adapters",
		Example: `
# Get the Mixer adapter configs for the given scope.
istioctl mixer adapter get global
`,
		RunE: mixerAdapterOrDescriptorGetRunE,
	}

	mixerDescriptorCmd = &cobra.Command{
		Use:          "descriptor",
		Short:        "Istio Mixer Descriptor configuration",
		Long:         "Create and list Mixer descriptors in the configuration server.",
		SilenceUsage: true,
	}

	mixerDescriptorCreateCmd = &cobra.Command{
		Use:   "create <scope>",
		Short: "Create Istio Mixer descriptors",
		Example: `
# Create new Mixer descriptor configs for the given scope.
istioctl mixer descriptor create global -f adapters.yml
`,
		RunE: mixerAdapterOrDescriptorCreateRunE,
	}

	mixerDescriptorGetCmd = &cobra.Command{
		Use:   "get <scope>",
		Short: "Get Istio Mixer descriptors",
		Example: `
# Get the Mixer descriptor configs for the given scope.
istioctl mixer descriptor get global
`,
		RunE: mixerAdapterOrDescriptorGetRunE,
	}
)

func mixerGet(path string) (string, error) {
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

func mixerRequest(method, path string, reqBody []byte) error {
	status, respBody, err := mixerRESTRequester.Request(method, path, reqBody)

	// If we got output, let's look at it, even if we got an error.  The output might include the reason for the error.
	if respBody != nil {
		var response mixerAPIResponse
		message := "unknown"
		if errJSON := json.Unmarshal(respBody, &response); errJSON == nil {
			message = response.Status.Message
		}

		if status != http.StatusOK {
			return fmt.Errorf("failed to %s %s with status %v: %s", method, path, status, message)
		}

		fmt.Printf("%s\n", message)
	}

	return err
}

func mixerRulePath(scope, subject string) string {
	return scopesPath + fmt.Sprintf("%s/subjects/%s/rules", url.PathEscape(scope), url.PathEscape(subject))
}

func mixerRuleCreate(scope, subject string, rule []byte) error {
	return mixerRequest(http.MethodPut, mixerRulePath(scope, subject), rule)
}

func mixerRuleGet(scope, subject string) (string, error) {
	return mixerGet(mixerRulePath(scope, subject))
}

func mixerRuleDelete(scope, subject string) error {
	return mixerRequest(http.MethodDelete, mixerRulePath(scope, subject), nil)
}

func mixerAdapterOrDescriptorPath(scope, name string) string {
	return scopesPath + fmt.Sprintf("%s/%s", url.PathEscape(scope), url.PathEscape(name))
}

func mixerAdapterOrDescriptorCreate(scope, name string, config []byte) error {
	path := mixerAdapterOrDescriptorPath(scope, name)
	return mixerRequest(http.MethodPut, path, config)
}

func mixerAdapterOrDescriptorGet(scope, name string) (string, error) {
	path := mixerAdapterOrDescriptorPath(scope, name)
	return mixerGet(path)
}

func mixerAdapterOrDescriptorCreateRunE(c *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New(c.UsageString())
	}
	return mixerAdapterOrDescriptorCreate(args[0], c.Parent().Name()+"s", mixerFileContent)
}

func mixerAdapterOrDescriptorGetRunE(c *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New(c.UsageString())
	}
	out, err := mixerAdapterOrDescriptorGet(args[0], c.Parent().Name()+"s")
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}

func init() {
	mixerRuleCreateCmd.PersistentFlags().StringVarP(&mixerFile, "file", "f", "",
		"Input file with contents of the Mixer rule")
	mixerAdapterCreateCmd.PersistentFlags().StringVarP(&mixerFile, "file", "f", "",
		"Input file with contents of the adapters config")
	mixerDescriptorCmd.PersistentFlags().StringVarP(&mixerFile, "file", "f", "",
		"Input file with contents of the descriptors config")
	mixerCmd.PersistentFlags().StringVar(&istioMixerAPIService,
		"mixerAPIService", "istio-mixer:9094",
		"Name of istio-mixer service. When --kube=false this sets the address of the mixer service")
	// TODO remove this flag once istio/istio integration tests are
	// updated to use mixer service
	mixerCmd.PersistentFlags().StringVar(&mixerAPIServerAddr, "mixer", os.Getenv("ISTIO_MIXER_API_SERVER"),
		"(deprecated) Address of the Mixer configuration server as <host>:<port>")

	mixerRuleCmd.AddCommand(mixerRuleCreateCmd)
	mixerRuleCmd.AddCommand(mixerRuleGetCmd)
	mixerRuleCmd.AddCommand(mixerRuleDeleteCmd)
	mixerCmd.AddCommand(mixerRuleCmd)
	mixerAdapterCmd.AddCommand(mixerAdapterCreateCmd)
	mixerAdapterCmd.AddCommand(mixerAdapterGetCmd)
	mixerCmd.AddCommand(mixerAdapterCmd)
	mixerDescriptorCmd.AddCommand(mixerDescriptorCreateCmd)
	mixerDescriptorCmd.AddCommand(mixerDescriptorGetCmd)
	mixerCmd.AddCommand(mixerDescriptorCmd)
	rootCmd.AddCommand(mixerCmd)
}
