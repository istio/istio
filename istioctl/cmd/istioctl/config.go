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
	"bytes"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"

	"istio.io/istio/pkg/log"
)

var (

	// TODO - Add diff option to get the difference between pilot's xDS API response and the proxy config
	// TODO - Add a full mesh Pilot look up (e.g. All clusters, all endpoints, etc)
	// TODO - Add support for non-default proxy config locations
	// TODO - Add support for non-kube istio deployments
	configCmd = &cobra.Command{
		Use:   "proxy-config <local|pilot> <pod-name> [<configuration-type>]",
		Short: "Retrieves proxy configuration for the specified pod from the local proxy or Pilot [kube only]",
		Long: `
Retrieves proxy configuration for the specified pod from the local proxy or Pilot when running in Kubernetes.

Available configuration types:

	Local:
	[clusters listeners routes static]

	Pilot:
	[clusters listeners routes endpoints]

`,
		Example: `# Retrieve all config for productpage-v1-bb8d5cbc7-k7qbm pod from the local proxy
istioctl proxy-config local productpage-v1-bb8d5cbc7-k7qbm

# Retrieve cluster config for productpage-v1-bb8d5cbc7-k7qbm pod from Pilot
istioctl proxy-config pilot productpage-v1-bb8d5cbc7-k7qbm clusters

# Retrieve static config for productpage-v1-bb8d5cbc7-k7qbm pod in the application namespace from the local proxy
istioctl proxy-config local -n application productpage-v1-bb8d5cbc7-k7qbm static`,
		Aliases: []string{"pc"},
		Args:    cobra.MinimumNArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			location := args[0]
			podName := args[1]
			var configType string
			if len(args) > 2 {
				configType = args[2]
			} else {
				configType = "all"
			}
			log.Infof("Retrieving %v proxy config for %q", configType, podName)

			ns := namespace
			if ns == v1.NamespaceAll {
				ns = defaultNamespace
			}
			var debug string
			if location == "pilot" {
				proxyID, pilotErr := getProxyID(podName, ns)
				if pilotErr != nil {
					return pilotErr
				}
				pilots, pilotErr := listPilot()
				if pilotErr != nil {
					return pilotErr
				}
				if len(pilots) == 0 {
					return errors.New("unable to find any Pilot instances")
				}
				debug, pilotErr = callPilotDiscoveryDebug(pilots[0].Name, pilots[0].Namespace, proxyID, configType)
				if pilotErr != nil {
					return pilotErr
				}
			} else if location == "local" {
				var localErr error
				debug, localErr = callPilotAgentDebug(podName, ns, configType)
				if localErr != nil {
					return localErr
				}
			}

			fmt.Println(debug)
			return nil
		},
	}
)

func init() {
	rootCmd.AddCommand(configCmd)
}

func createCoreV1Client() (*rest.RESTClient, error) {
	config, err := defaultRestConfig()
	if err != nil {
		return nil, err
	}
	return rest.RESTClientFor(config)
}

func defaultRestConfig() (*rest.Config, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	config.APIPath = "/api"
	config.GroupVersion = &v1.SchemeGroupVersion
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: scheme.Codecs}
	return config, nil
}

func getProxyID(podName, podNamespace string) (string, error) {
	cmd := []string{"/usr/local/bin/pilot-agent", "get", "proxyID"}
	if stdout, stderr, err := podExec(podName, podNamespace, "istio-proxy", cmd); err != nil {
		return "", err
	} else if stderr.String() != "" {
		return "", fmt.Errorf("unable to call retrieve proxy ID: %v", stderr.String())
	} else {
		return stdout.String(), nil
	}
}

func callPilotDiscoveryDebug(podName, podNamespace, proxyID, configType string) (string, error) {
	cmd := []string{"/usr/local/bin/pilot-discovery", "debug", proxyID, configType}
	if stdout, stderr, err := podExec(podName, podNamespace, "discovery", cmd); err != nil {
		return "", err
	} else if stderr.String() != "" {
		return "", fmt.Errorf("unable to call pilot-disocver debug: %v", stderr.String())
	} else {
		return stdout.String(), nil
	}
}

func callPilotAgentDebug(podName, podNamespace, configType string) (string, error) {
	cmd := []string{"/usr/local/bin/pilot-agent", "debug", configType}
	if stdout, stderr, err := podExec(podName, podNamespace, "istio-proxy", cmd); err != nil {
		return "", err
	} else if stderr.String() != "" {
		return "", fmt.Errorf("unable to call pilot-agent debug: %v", stderr.String())
	} else {
		return stdout.String(), nil
	}
}

func listPilot() ([]v1.Pod, error) {
	client, err := createCoreV1Client()
	if err != nil {
		return nil, err
	}

	req := client.Get().
		Resource("pods").
		Namespace(istioNamespace).
		Param("labelSelector", "istio=pilot")

	res := req.Do()
	if res.Error() != nil {
		return nil, fmt.Errorf("unable to retrieve Pods: %v", res.Error())
	}
	list := &v1.PodList{}
	if err := res.Into(list); err != nil {
		return nil, fmt.Errorf("unable to parse PodList: %v", res.Error())
	}
	return list.Items, nil
}

func podExec(podName, podNamespace, container string, command []string) (*bytes.Buffer, *bytes.Buffer, error) {
	client, err := createCoreV1Client()
	if err != nil {
		return nil, nil, err
	}

	req := client.Post().
		Resource("pods").
		Name(podName).
		Namespace(podNamespace).
		SubResource("exec").
		Param("container", container).
		VersionedParams(&v1.PodExecOptions{
			Container: "istio-proxy",
			Command:   command,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)
	config, err := defaultRestConfig()
	if err != nil {
		return nil, nil, err
	}

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return nil, nil, err
	}

	var stdout, stderr bytes.Buffer
	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	})

	return &stdout, &stderr, err
}
