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

package kube

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	kubelib "istio.io/istio/pkg/kube"
)

var (
	outputBufferSize  = 4096
	forwardRegex      = regexp.MustCompile("Forwarding from (127.0.0.1:[0-9]+) -> ([0-9]+)")
	addressMatchIndex = 1
)

// PortForwarder is a wrapper for port forward.
type PortForwarder interface {
	ForwardPorts() error
	Address() string
	Close()
}

type defaultPortForwarder struct {
	forwarder *portforward.PortForwarder
	readyCh   <-chan struct{}
	address   string
	output    *bytes.Buffer
}

// PodSelectOptions contains options for pod selection.
// Must specify PodNamespace and one of PodName/LabelSelector.
type PodSelectOptions struct {
	PodNamespace  string
	PodName       string
	LabelSelector string
}

func (f *defaultPortForwarder) ForwardPorts() error {
	errCh := make(chan error)
	go func() {
		errCh <- f.forwarder.ForwardPorts()
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("failure running port forward process: %v", err)
	case <-f.readyCh:
		address, err := extractAddress(f.output.String())
		if err != nil {
			return err
		}
		f.address = address
		return nil
	}
}

func (f *defaultPortForwarder) Address() string {
	return f.address
}

func (f *defaultPortForwarder) Close() {
	f.forwarder.Close()
}

// NewPortForwarder return a new PortForwarder instance.
func NewPortForwarder(client *rest.RESTClient, config *rest.Config, options *PodSelectOptions, ports string) (PortForwarder, error) {
	if err := validatePodSelectOptions(options); err != nil {
		return nil, err
	}
	podName := options.PodName
	if podName == "" {
		// Retrieve pod according to labelSelector if pod name not specified.
		pod, err := getSelectedPod(client, options)
		if err != nil {
			return nil, err
		}
		podName = pod.Name
	}

	req := client.Post().Resource("pods").Namespace(options.PodNamespace).Name(podName).SubResource("portforward")

	transport, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		return nil, fmt.Errorf("failure creating roundtripper: %v", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())

	stopCh := make(chan struct{})
	readyCh := make(chan struct{})
	output := bytes.NewBuffer(make([]byte, 0, outputBufferSize))
	fw, err := portforward.New(dialer, []string{ports}, stopCh, readyCh, output, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("failed establishing port-forward: %v", err)
	}

	return &defaultPortForwarder{
		forwarder: fw,
		readyCh:   readyCh,
		output:    output,
	}, nil
}

// getSelectedPod retrieves pods according to given options.
func getSelectedPod(client *rest.RESTClient, options *PodSelectOptions) (*v1.Pod, error) {
	podGet := client.Get().Resource("pods").Namespace(options.PodNamespace).Param("labelSelector", options.LabelSelector)
	obj, err := podGet.Do().Get()
	if err != nil {
		return nil, fmt.Errorf("failed retrieving pod: %v", err)
	}

	podList := obj.(*v1.PodList)
	if len(podList.Items) < 1 {
		return nil, errors.New("no corresponding pods found")
	}

	return &podList.Items[0], nil
}

// validatePodSelectOptions test if given PodSelectOptions is valid.
func validatePodSelectOptions(options *PodSelectOptions) error {
	if options.PodNamespace == "" {
		return fmt.Errorf("no pod namespace specified")
	}
	if options.PodName == "" && options.LabelSelector == "" {
		return fmt.Errorf("neither pod name nor label selector specified")
	}

	return nil
}

func extractAddress(output string) (string, error) {
	// TODO: improve this when we have multi port inputs.
	matches := forwardRegex.FindStringSubmatch(output)
	if matches == nil {
		return "", fmt.Errorf("failed to get address from output: %s", output)
	}
	return matches[addressMatchIndex], nil
}

// NewRestClient return rest client with default config.
// TODO: remove this when we have common rest client library in test-framework.
func NewRestClient(kubeconfig, configContext string) (*rest.RESTClient, *rest.Config, error) {
	config, err := defaultRestConfig(kubeconfig, configContext)
	if err != nil {
		return nil, nil, err
	}
	restClient, err := rest.RESTClientFor(config)
	if err != nil {
		return nil, nil, err
	}
	return restClient, config, nil
}

func defaultRestConfig(kubeconfig, configContext string) (*rest.Config, error) {
	config, err := kubelib.BuildClientConfig(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}
	config.APIPath = "/api"
	config.GroupVersion = &v1.SchemeGroupVersion
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: scheme.Codecs}
	return config, nil
}

// PortForward is an utility function to help construct port forwarder.
// TODO(lichuqiang): find a better place for it.
func PortForward(kubeconfig string, options *PodSelectOptions, localPort, remotePort string) (PortForwarder, error) {
	rc, config, err := NewRestClient(kubeconfig, "")
	if err != nil {
		return nil, fmt.Errorf("failed to create kube client: %v", err)
	}

	ports := localPort + ":" + remotePort
	forwarder, err := NewPortForwarder(rc, config, options, ports)
	if err != nil {
		return nil, fmt.Errorf("failed to create port forwarder: %v", err)
	}

	if err := forwarder.ForwardPorts(); err != nil {
		return nil, fmt.Errorf("error in port forward: %v", err)
	}

	return forwarder, nil
}
