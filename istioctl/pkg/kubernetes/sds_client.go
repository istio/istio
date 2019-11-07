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

package kubernetes

import (
	"encoding/json"
	"fmt"

	"istio.io/istio/security/pkg/nodeagent/sds"
	"istio.io/pkg/log"

	v1 "k8s.io/api/core/v1"
)

const (
	ingressGatewayApp = "istio-ingressgateway"
	debugEndpointPath = "localhost:8080/debug/sds"
)

// ExecClientSDS wraps the kubernetes API and provides needed access for sds-status command
type ExecClientSDS interface {
	GetPodNodeAgentSecrets(string, string, string) (map[string]sds.Debug, error)
	NodeAgentDebugEndpointOutput(string, string, string, string) (sds.Debug, error)
	ExecClient
}

// GetPodNodeAgentSecrets, given a pod name, finds each of a pod's corresponding node agents, hits the debug endpoint,
// and then returns a map with key => node agent pod name, value => sds.Debug from that node
func (client *Client) GetPodNodeAgentSecrets(podName, ns, istioNamespace string) (map[string]sds.Debug, error) {
	debugResponses := make(map[string]sds.Debug)
	nodeAgents, err := client.nodeAgentsForPod(podName, ns, istioNamespace)
	if err != nil {
		return nil, err
	}
	if len(nodeAgents) == 0 {
		return nil, fmt.Errorf("could not find Node Agent for supplied pod %s.%s",
			podName, ns)
	}

	for _, agent := range nodeAgents {
		secretType := "workload"
		container := "" // for a normal node agent, there is no additional container in the pod
		if isIngressGateway(agent) {
			secretType = "gateway"
			container = "istio-proxy"
		}

		debugOutput, err := client.NodeAgentDebugEndpointOutput(
			agent.Name, agent.Namespace, secretType, container)
		if err != nil {
			return nil, err
		}

		debugResponses[agent.Name] = debugOutput
	}
	return debugResponses, nil
}

// NodeAgentDebugEndpointOutput makes a request to the target nodeagent's debug endpoint and returns the raw response
func (client *Client) NodeAgentDebugEndpointOutput(podName, ns, secretType, container string) (sds.Debug, error) {
	request := []string{
		"curl",
		fmt.Sprintf("%s/%s", debugEndpointPath, secretType),
	}
	rawResult, err := client.ExtractExecResult(podName, ns, container, request)
	if err != nil {
		return sds.Debug{}, err
	}

	var sdsDebug sds.Debug
	err = json.Unmarshal(rawResult, &sdsDebug)
	if err != nil {
		return sds.Debug{}, fmt.Errorf("failed to unmarshal node agent debug response: %v", err)
	}

	return sdsDebug, nil
}

// nodeAgentsForPod returns all node agents which are serving secrets to the supplied pod
// in the case of an ingress-gateway, it is possible for there to be are two corresponding nodeagents: the one on the node
// running as a daemon set serving workload secrets, and the embedded nodeagent running in the same pod
func (client *Client) nodeAgentsForPod(name, ns, istioNamespace string) ([]*v1.Pod, error) {
	agentPods := make([]*v1.Pod, 0)

	// need to retrieve more information on target pod, such as node and labels
	podGet := client.Get().Resource("pods").Namespace(ns).Name(name)
	obj, err := podGet.Do().Get()
	if err != nil {
		log.Debugf("failed to retrieve pod information for %s.%s: %v", ns, name, err)
		return nil, err
	}
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return nil, fmt.Errorf("failed retrieving pod: %s", name)
	}

	// if the pod is a secure ingress-gateway, we should add the pod itself, since its using an embedded nodeagent
	if isIngressGateway(pod) && len(pod.Spec.Containers) > 1 {
		agentPods = append(agentPods, pod)
	}

	nodeAgentFilters := map[string]string{
		"labelSelector": "app=nodeagent",
		"fieldSelector": "spec.nodeName=" + pod.Spec.NodeName,
	}
	nodeAgentPodReq := client.Get().
		Resource("pods").
		Namespace(istioNamespace)
	for k, v := range nodeAgentFilters {
		nodeAgentPodReq.Param(k, v)
	}

	res := nodeAgentPodReq.Do()
	if res.Error() != nil {
		log.Debugf("failed to retrieve node agent pods: %v", err)
		return nil, fmt.Errorf("unable to retrieve node agent pods: %v", res.Error())
	}
	nodeAgentPods := &v1.PodList{}
	if err := res.Into(nodeAgentPods); err != nil {
		return nil, fmt.Errorf("unable to parse response into pod list: %v", res.Error())
	}
	if len(nodeAgentPods.Items) == 1 {
		agentPods = append(agentPods, &nodeAgentPods.Items[0])
	}

	return agentPods, nil
}

func isIngressGateway(p *v1.Pod) bool {
	appLabel, ok := p.Labels["app"]
	return ok && appLabel == ingressGatewayApp
}
