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

package xds

import (
	"strings"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/kube/labels"
	wmpb "istio.io/istio/pkg/workloadmetadata/proto"
)

const (
	// TODO: adjust to whatever full scheme is adopted
	// TODO: move to the xds package, along with configuration for
	WorkloadMetadataListenerFilterName = "ambient.workload_metadata"
)

// WorkloadMetadataGenerator is responsible for generating dynamic Ambient Listener Filter
// configurations. These configurations include workload metadata for individual
// workload instances (Kubernetes pods) running on a Kubernetes node.
type WorkloadMetadataGenerator struct {
	Server *DiscoveryServer
}

var _ model.XdsResourceGenerator = &WorkloadMetadataGenerator{}

func (g *WorkloadMetadataGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (
	model.Resources, model.XdsLogDetails, error,
) {
	// TODO: check whether or not a push is required?
	// Need to figure out how to push to a node based on deltas in pods on a node

	// this is the name of the Kubernetes node on which the proxy requesting this
	// configuration lives.
	proxyKubernetesNodeName := ""

	nodesToWorkloads := map[string][]*wmpb.WorkloadMetadataResource{}
	for _, pod := range g.Server.Env.PodInformation() {

		// TODO: this is cheating. we need a way to get the owing workload name
		// in a way that isn't a shortcut.
		name, workloadType := workloadNameAndType(pod)
		cs, cr := labels.CanonicalService(pod.Labels, name)

		ips := []string{}
		for _, pip := range pod.Status.PodIPs {
			ips = append(ips, pip.IP)
		}

		// TODO: is there a better way to find the node for the proxy calling in?
		if cmp.Equal(ips, proxy.IPAddresses) {
			proxyKubernetesNodeName = pod.Spec.NodeName
		}

		containers := []string{}
		for _, c := range pod.Spec.Containers {
			containers = append(containers, c.Name)
		}

		nodesToWorkloads[pod.Spec.NodeName] = append(nodesToWorkloads[pod.Spec.NodeName],
			&wmpb.WorkloadMetadataResource{
				IpAddresses:       ips,
				InstanceName:      pod.Name,
				NamespaceName:     pod.Namespace,
				Containers:        containers,
				WorkloadName:      name,
				WorkloadType:      workloadType,
				CanonicalName:     cs,
				CanonicalRevision: cr,
			})
	}

	wmd := &wmpb.WorkloadMetadataResources{
		ProxyId:                   proxy.ID,
		WorkloadMetadataResources: nodesToWorkloads[proxyKubernetesNodeName],
	}

	resources := model.Resources{&discovery.Resource{Resource: util.MessageToAny(wmd)}}
	return resources, model.DefaultXdsLogDetails, nil
}

// total hack
func workloadNameAndType(pod *v1.Pod) (string, wmpb.WorkloadMetadataResource_WorkloadType) {
	if len(pod.GenerateName) == 0 {
		return pod.Name, wmpb.WorkloadMetadataResource_KUBERNETES_POD
	}

	// if the pod name was generated (or is scheduled for generation), we can begin an investigation into the controlling reference for the pod.
	var controllerRef metav1.OwnerReference
	controllerFound := false
	for _, ref := range pod.GetOwnerReferences() {
		if *ref.Controller {
			controllerRef = ref
			controllerFound = true
			break
		}
	}

	if !controllerFound {
		return pod.Name, wmpb.WorkloadMetadataResource_KUBERNETES_POD
	}

	// heuristic for deployment detection
	if controllerRef.Kind == "ReplicaSet" && strings.HasSuffix(controllerRef.Name, pod.Labels["pod-template-hash"]) {
		name := strings.TrimSuffix(controllerRef.Name, "-"+pod.Labels["pod-template-hash"])
		return name, wmpb.WorkloadMetadataResource_KUBERNETES_DEPLOYMENT
	}

	if controllerRef.Kind == "Job" {
		// figure out how to go from Job -> CronJob
		return controllerRef.Name, wmpb.WorkloadMetadataResource_KUBERNETES_JOB
	}

	if controllerRef.Kind == "CronJob" {
		// figure out how to go from Job -> CronJob
		return controllerRef.Name, wmpb.WorkloadMetadataResource_KUBERNETES_CRONJOB
	}

	return pod.Name, wmpb.WorkloadMetadataResource_KUBERNETES_POD
}
