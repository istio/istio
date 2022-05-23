//go:build gofuzz
// +build gofuzz

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

package controller

import (
	"context"

	fuzz "github.com/AdaLogics/go-fuzz-headers"
	coreV1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/network"
	"istio.io/istio/tests/fuzz/utils"
)

func InternalFuzzKubeController(data []byte) int {
	networkID := network.ID("fakeNetwork")
	fco := FakeControllerOptions{}
	f := fuzz.NewConsumer(data)
	f.AllowUnexportedFields()
	err := f.GenerateStruct(&fco)
	if err != nil {
		return 0
	}
	t := &utils.NopTester{}
	controller, fx := NewFakeControllerWithOptions(t, fco)
	controller.network = networkID
	defer controller.Stop()

	p, err := generatePodFuzz(f)
	if err != nil {
		return 0
	}
	err = addPodsForFuzzing(controller, fx, p)
	if err != nil {
		return 0
	}

	err = createServiceForFuzzing(controller, f)
	if err != nil {
		return 0
	}

	err = createEndpointsForFuzzing(f, controller)
	if err != nil {
		return 0
	}

	node, err := generateNodeForFuzzing(f)
	if err != nil {
		return 0
	}

	err = addNodesForFuzzing(controller, node)
	if err != nil {
		return 0
	}
	return 1
}

func generatePodFuzz(f *fuzz.ConsumeFuzzer) (*coreV1.Pod, error) {
	pod := &coreV1.Pod{}
	return pod, f.GenerateStruct(pod)
}

func generateNodeForFuzzing(f *fuzz.ConsumeFuzzer) (*coreV1.Node, error) {
	node := &coreV1.Node{}
	return node, f.GenerateStruct(node)
}

func addPodsForFuzzing(controller *FakeController, fx *FakeXdsUpdater, pods ...*coreV1.Pod) error {
	for _, pod := range pods {
		p, _ := controller.client.Kube().CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metaV1.GetOptions{})
		var newPod *coreV1.Pod
		var err error
		if p == nil {
			newPod, err = controller.client.Kube().CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metaV1.CreateOptions{})
			if err != nil {
				return err
			}
		} else {
			newPod, err = controller.client.Kube().CoreV1().Pods(pod.Namespace).Update(context.Background(), pod, metaV1.UpdateOptions{})
			if err != nil {
				return err
			}
		}

		setPodReadyForFuzzing(newPod)
		newPod.Status.PodIP = pod.Status.PodIP
		newPod.Status.Phase = coreV1.PodRunning
		_, _ = controller.client.Kube().CoreV1().Pods(pod.Namespace).UpdateStatus(context.Background(), newPod, metaV1.UpdateOptions{})
		fx.Wait("proxy")
	}
	return nil
}

func setPodReadyForFuzzing(pod *coreV1.Pod) {
	pod.Status.Conditions = []coreV1.PodCondition{
		{
			Type:               coreV1.PodReady,
			Status:             coreV1.ConditionTrue,
			LastTransitionTime: metaV1.Now(),
		},
	}
}

func addNodesForFuzzing(controller *FakeController, nodes ...*coreV1.Node) error {
	fakeClient := controller.client
	for _, node := range nodes {
		_, err := fakeClient.Kube().CoreV1().Nodes().Create(context.Background(), node, metaV1.CreateOptions{})
		if errors.IsAlreadyExists(err) {
			if _, err := fakeClient.Kube().CoreV1().Nodes().Update(context.Background(), node, metaV1.UpdateOptions{}); err != nil {
				return nil
			}
		} else if err != nil {
			return err
		}
	}
	return nil
}

func createServiceForFuzzing(controller *FakeController, f *fuzz.ConsumeFuzzer) error {
	service := &coreV1.Service{}
	namespace, err := f.GetString()
	if err != nil {
		return err
	}
	err = f.GenerateStruct(service)
	if err != nil {
		return err
	}
	_, err = controller.client.Kube().CoreV1().Services(namespace).Create(context.Background(), service, metaV1.CreateOptions{})
	if err != nil {
		return err
	}
	return nil
}

func createEndpointsForFuzzing(f *fuzz.ConsumeFuzzer, controller *FakeController) error {
	labels := make(map[string]string)
	// Add the reference to the service. Used by EndpointSlice logic only.
	name, err := f.GetString()
	if err != nil {
		return err
	}
	labels[discovery.LabelServiceName] = name

	eas := make([]coreV1.EndpointAddress, 0)
	number, err := f.GetInt()
	if err != nil {
		return err
	}
	for i := 0; i < number%50; i++ {
		ea := coreV1.EndpointAddress{}
		err = f.GenerateStruct(&ea)
		if err != nil {
			return err
		}
		eas = append(eas, ea)
	}

	eps := make([]coreV1.EndpointPort, 0)
	number, err = f.GetInt()
	if err != nil {
		return err
	}
	for i := 0; i < number%50; i++ {
		ep := coreV1.EndpointPort{}
		err = f.GenerateStruct(&ep)
		if err != nil {
			return err
		}
		eps = append(eps, ep)
	}

	endpoint := &coreV1.Endpoints{}
	err = f.GenerateStruct(endpoint)
	if err != nil {
		return err
	}
	namespace, err := f.GetString()
	if err != nil {
		return err
	}
	if _, err := controller.client.Kube().CoreV1().Endpoints(namespace).Create(context.Background(), endpoint, metaV1.CreateOptions{}); err != nil {
		if errors.IsAlreadyExists(err) {
			_, err = controller.client.Kube().CoreV1().Endpoints(namespace).Update(context.Background(), endpoint, metaV1.UpdateOptions{})
		}
		if err != nil {
			return err
		}
	}

	endpointSlice := &discovery.EndpointSlice{}
	err = f.GenerateStruct(endpointSlice)
	if err != nil {
		return err
	}
	if _, err := controller.client.Kube().DiscoveryV1().EndpointSlices(namespace).Create(context.Background(), endpointSlice, metaV1.CreateOptions{}); err != nil {
		if errors.IsAlreadyExists(err) {
			_, err = controller.client.Kube().DiscoveryV1().EndpointSlices(namespace).Update(context.Background(), endpointSlice, metaV1.UpdateOptions{})
		}
		if err != nil {
			return err
		}
	}
	return nil
}
