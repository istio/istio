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

const (
	discoveryContainer = "discovery"
	proxyContainer     = "istio-proxy"
	mesh               = "all"
)

var (

	// TODO - Add diff option to get the difference between pilot's xDS API response and the proxy config
	// TODO - Add support for non-default proxy config locations
	// TODO - Add support for non-kube istio deployments
	// TODO - Bring Endpoint and Pilot config types more inline with each other
	configCmd = &cobra.Command{
		Use:   "proxy-config <endpoint|pilot> <pod-name|mesh> [<configuration-type>]",
		Short: "Retrieves proxy configuration for the specified pod from the endpoint proxy or Pilot [kube only]",
		Long: `
Retrieves proxy configuration for the specified pod from the endpoint proxy or Pilot when running in Kubernetes.
It is also able to retrieve the state of the entire mesh by using mesh instead of <pod-name>. This is only available when querying Pilot.

Available configuration types:

	Endpoint:
	[clusters listeners routes static]

	Pilot:
	[ads eds]

`,
		Example: `# Retrieve all config for productpage-v1-bb8d5cbc7-k7qbm pod from the endpoint proxy
istioctl proxy-config endpoint productpage-v1-bb8d5cbc7-k7qbm

# Retrieve eds config for productpage-v1-bb8d5cbc7-k7qbm pod from Pilot
istioctl proxy-config pilot productpage-v1-bb8d5cbc7-k7qbm eds

# Retrieve ads config for the mesh from Pilot
istioctl proxy-config pilot mesh ads

# Retrieve static config for productpage-v1-bb8d5cbc7-k7qbm pod in the application namespace from the endpoint proxy
istioctl proxy-config endpoint -n application productpage-v1-bb8d5cbc7-k7qbm static`,
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
			switch location {
			case "pilot":
				var proxyID string
				if podName == "mesh" {
					proxyID = mesh
				} else {
					proxyID = fmt.Sprintf("%v.%v", podName, ns)
				}
				pilots, err := getPilotPods()
				if err != nil {
					return err
				}
				if len(pilots) == 0 {
					return errors.New("unable to find any Pilot instances")
				}
				debug, pilotErr := callPilotDiscoveryDebug(pilots, proxyID, configType)
				if pilotErr != nil {
					fmt.Println(debug)
					return err
				}
				fmt.Println(debug)
			case "endpoint":
				debug, err := callPilotAgentDebug(podName, ns, configType)
				if err != nil {
					fmt.Println(debug)
					return err
				}
				fmt.Println(debug)
			default:
				log.Errorf("%q debug not supported", location)
			}
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

func callPilotDiscoveryDebug(pods []v1.Pod, proxyID, configType string) (string, error) {
	cmd := []string{"/usr/local/bin/pilot-discovery", "debug", proxyID, configType}
	var err error
	var stdout, stderr *bytes.Buffer
	for _, pod := range pods {
		fmt.Println(pod.Name, configType)
		stdout, stderr, err = podExec(pod.Name, pod.Namespace, discoveryContainer, cmd)
		if stdout.String() != "" {
			if proxyID != mesh {
				return stdout.String(), nil
			}
			fmt.Println(stdout.String())
		}
	}
	if stderr.String() != "" {
		return "", fmt.Errorf("unable to call pilot-discover debug: %v", stderr.String())
	}
	return stdout.String(), err
}

func callPilotAgentDebug(podName, podNamespace, configType string) (string, error) {
	cmd := []string{"/usr/local/bin/pilot-agent", "debug", configType}
	container, err := getPilotAgentContainer(podName, podNamespace)
	if err != nil {
		return "", fmt.Errorf("unable to retrieve proxy container name: %v", err)
	}
	stdout, stderr, err := podExec(podName, podNamespace, container, cmd)
	if err != nil {
		return stdout.String(), err
	} else if stderr.String() != "" {
		return "", fmt.Errorf("unable to call pilot-agent debug: %v", stderr.String())
	}
	return stdout.String(), nil
}

func getPilotAgentContainer(podName, podNamespace string) (string, error) {
	client, err := createCoreV1Client()
	if err != nil {
		return "", err
	}

	req := client.Get().
		Resource("pods").
		Namespace(podNamespace).
		Name(podName)

	res := req.Do()
	if res.Error() != nil {
		return "", fmt.Errorf("unable to retrieve Pod: %v", res.Error())
	}
	pod := &v1.Pod{}
	if err := res.Into(pod); err != nil {
		return "", fmt.Errorf("unable to parse Pod: %v", res.Error())
	}
	for _, c := range pod.Spec.Containers {
		switch c.Name {
		case "egressgateway", "ingress", "ingressgateway":
			return c.Name, nil
		}
	}
	return proxyContainer, nil
}

func getPilotPods() ([]v1.Pod, error) {
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
			Container: container,
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
