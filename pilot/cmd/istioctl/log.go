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
	"fmt"
	"io"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/pkg/log"
)

type component struct {
	Name      string
	Container string
	Label     string
}

var (
	controlPlaneComponents = map[string]component{
		"pilot":   {Name: "pilot", Container: "discovery", Label: "istio=pilot"},
		"mixer":   {Name: "mixer", Container: "mixer", Label: "istio=mixer"},
		"ingress": {Name: "ingress", Container: "", Label: "istio=ingress"},
		"ca":      {Name: "ca", Container: "", Label: "istio=istio-ca"},
	}

	// TODO - allow retrieval of proxy logs when using istioctl logs proxy <podID> [-n namespace]
	logCmd = &cobra.Command{
		Use:   "logs <component> [container]",
		Short: "Retrieves logs from an Istio component [kube only]",
		Long: `
Retrieves logs from an Istio component when running in Kubernetes
	
Supported components are:

	[pilot mixer ingress ca]

		`,
		Example: `
	# Retrieve the logs to Pilot's primary container (discovery)
	istioctl logs pilot

	# Retrieve logs from the specified container in Pilot's pod
	istioctl logs pilot istio-proxy
		`,
		Aliases: []string{"log"},
		Args:    cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			componentName := args[0]
			log.Infof("Retrieving logs for %q", componentName)

			client, err := createCoreV1Client()
			if err != nil {
				return err
			}

			comp, ok := controlPlaneComponents[componentName]
			if !ok {
				return fmt.Errorf("%q does not match known list of components. Use istioctl logs --help for supported list", componentName)
			}
			if len(args) == 2 {
				if comp.Container != "" && comp.Container != args[1] {
					comp.Container = args[1]
				}
			}

			pod, err := retrieveComponentPod(client, comp, istioNamespace)
			if err != nil {
				return err
			}

			stream, err := retrievePodLogStream(client, comp, pod)
			if err != nil {
				return err
			}

			defer stream.Close()
			_, err = io.Copy(os.Stdout, stream)
			return err
		},
	}
)

func init() {
	rootCmd.AddCommand(logCmd)
}

func createCoreV1Client() (*rest.RESTClient, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	config.APIPath = "/api"
	config.GroupVersion = &corev1.SchemeGroupVersion
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: scheme.Codecs}
	return rest.RESTClientFor(config)
}

func retrieveComponentPod(client cache.Getter, comp component, namespace string) (*corev1.Pod, error) {
	podReq := client.Get().
		Namespace(namespace).
		Resource("pods").
		Param("labelSelector", comp.Label).
		Param("container", "discovery")
	podRes := podReq.Do()
	podList, err := podRes.Get()
	if err != nil {
		return nil, err
	}

	pods := podList.(*corev1.PodList).Items
	if len(pods) != 1 {
		return nil, fmt.Errorf("expected to find one %q pod, but got %v", comp.Name, len(pods))
	}
	return &pods[0], nil
}

func retrievePodLogStream(client cache.Getter, comp component, pod *corev1.Pod) (io.ReadCloser, error) {
	logReq := client.Get().
		Namespace(pod.ObjectMeta.Namespace).
		Resource("pods").
		Name(pod.GetName()).
		SubResource("log").
		Param("container", comp.Container)

	return logReq.Stream()
}
