// Copyright 2019 Istio Authors
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

package manifest

import (
	"fmt"

	"github.com/docker/distribution/reference"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"

	"istio.io/operator/pkg/util"
)

// Client is a helper wrapper around the Kube RESTClient for istioctl -> Pilot/Envoy/Mesh related things
type Client struct {
	Config *rest.Config
	*rest.RESTClient
}

// ComponentVersion is a pair of component name and version
type ComponentVersion struct {
	Component string
	Version   string
	Pod       v1.Pod
}

func (cv ComponentVersion) String() string {
	return fmt.Sprintf("%s pod - %s - version: %s",
		cv.Component, cv.Pod.GetName(), cv.Version)
}

// ExecClient is an interface for remote execution
type ExecClient interface {
	GetIstioVersions(namespace string) ([]ComponentVersion, error)
	GetPods(namespace string, params map[string]string) (*v1.PodList, error)
	PodsForSelector(namespace, labelSelector string) (*v1.PodList, error)
	ConfigMapForSelector(namespace, labelSelector string) (*v1.ConfigMapList, error)
}

// NewClient is the constructor for the client wrapper
func NewClient(kubeconfig, configContext string) (*Client, error) {
	config, err := defaultRestConfig(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}
	restClient, err := rest.RESTClientFor(config)
	if err != nil {
		return nil, err
	}
	return &Client{config, restClient}, nil
}

// GetIstioVersions gets the version for each Istio component
func (client *Client) GetIstioVersions(namespace string) ([]ComponentVersion, error) {
	pods, err := client.GetPods(namespace, map[string]string{
		"labelSelector": "istio",
		"fieldSelector": "status.phase=Running",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve Istio pods, error: %v", err)
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("istio pod not found in namespace %v", namespace)
	}

	var errs util.Errors
	var res []ComponentVersion
	for _, pod := range pods.Items {
		component := pod.Labels["istio"]

		switch component {
		case "statsd-prom-bridge":
			continue
		case "mixer":
			component = pod.Labels["istio-mixer-type"]
		}

		server := ComponentVersion{
			Component: component,
			Pod:       pod,
		}

		pv := ""
		for _, c := range pod.Spec.Containers {
			cv, err := parseTag(c.Image)
			if err != nil {
				errs = util.AppendErr(errs, err)
			}

			if pv == "" {
				pv = cv
			} else if pv != cv {
				err := fmt.Errorf("differrent versions of containers in the same pod: %v", pod.Spec.Containers)
				errs = util.AppendErr(errs, err)
			}
		}
		server.Version = pv
		res = append(res, server)
	}
	return res, errs.ToError()
}

func parseTag(image string) (string, error) {
	ref, err := reference.Parse(image)
	if err != nil {
		return "", fmt.Errorf("could not parse image: %s, error: %v", image, err)
	}

	switch t := ref.(type) {
	case reference.Tagged:
		return t.Tag(), nil
	default:
		return "", fmt.Errorf("tag not found in image: %v", image)
	}
}

func (client *Client) PodsForSelector(namespace, labelSelector string) (*v1.PodList, error) {
	pods, err := client.GetPods(namespace, map[string]string{
		"labelSelector": labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve pods, error: %v", err)
	}
	return pods, nil
}

// GetPods retrieves the pod objects for Istio deployments
func (client *Client) GetPods(namespace string, params map[string]string) (*v1.PodList, error) {
	req := client.Get().
		Resource("pods").
		Namespace(namespace)
	for k, v := range params {
		req.Param(k, v)
	}

	res := req.Do()
	if res.Error() != nil {
		return nil, fmt.Errorf("unable to retrieve Pods: %v", res.Error())
	}
	list := &v1.PodList{}
	if err := res.Into(list); err != nil {
		return nil, fmt.Errorf("unable to parse PodList: %v", res.Error())
	}
	return list, nil
}

func (client *Client) ConfigMapForSelector(namespace, labelSelector string) (*v1.ConfigMapList, error) {
	cmGet := client.Get().Resource("configmaps").Namespace(namespace).Param("labelSelector", labelSelector)
	obj, err := cmGet.Do().Get()
	if err != nil {
		return nil, fmt.Errorf("failed retrieving configmap: %v", err)
	}
	return obj.(*v1.ConfigMapList), nil
}
