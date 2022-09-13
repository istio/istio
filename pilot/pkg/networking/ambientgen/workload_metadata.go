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

package ambientgen

import (
	"strings"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/ambient"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/kube/labels"
	wmpb "istio.io/istio/pkg/workloadmetadata/proto"
)

const (
	WorkloadMetadataListenerFilterName = "envoy.filters.listener.workload_metadata"
	WorkloadMetadataResourcesTypeURL   = "type.googleapis.com/" + "istio.telemetry.workloadmetadata.v1.WorkloadMetadataResources"
)

// WorkloadMetadataGenerator is responsible for generating dynamic Ambient Listener Filter
// configurations. These configurations include workload metadata for individual
// workload instances (Kubernetes pods) running on a Kubernetes node.
type WorkloadMetadataGenerator struct{}

var _ model.XdsResourceGenerator = &WorkloadMetadataGenerator{}

func (g *WorkloadMetadataGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (
	model.Resources, model.XdsLogDetails, error,
) {
	// TODO: check whether or not a push is required?
	// Need to figure out how to push to a node based on deltas in pods on a node

	// this is the name of the Kubernetes node on which the proxy requesting this
	// configuration lives.
	proxyKubernetesNodeName := proxy.Metadata.NodeName

	var workloads []*wmpb.WorkloadMetadataResource

	for _, wl := range req.Push.AmbientIndex.Workloads.ByNode[proxyKubernetesNodeName] {
		// TODO: this is cheating. we need a way to get the owing workload name
		// in a way that isn't a shortcut.
		name, workloadType := workloadNameAndType(wl)
		cs, cr := labels.CanonicalService(wl.Labels, name)

		workloads = append(workloads,
			&wmpb.WorkloadMetadataResource{
				IpAddresses:       wl.PodIPs,
				InstanceName:      wl.Name,
				NamespaceName:     wl.Namespace,
				Containers:        wl.Containers,
				WorkloadName:      name,
				WorkloadType:      workloadType,
				CanonicalName:     cs,
				CanonicalRevision: cr,
			})
	}

	wmd := &wmpb.WorkloadMetadataResources{
		ProxyId:                   proxy.ID,
		WorkloadMetadataResources: workloads,
	}

	tec := &corev3.TypedExtensionConfig{
		Name:        WorkloadMetadataListenerFilterName,
		TypedConfig: protoconv.MessageToAny(wmd),
	}

	resources := model.Resources{&discovery.Resource{Resource: protoconv.MessageToAny(tec)}}
	return resources, model.DefaultXdsLogDetails, nil
}

// total hack
func workloadNameAndType(wl ambient.Workload) (string, wmpb.WorkloadMetadataResource_WorkloadType) {
	if len(wl.GenerateName) == 0 {
		return wl.Name, wmpb.WorkloadMetadataResource_KUBERNETES_POD
	}

	// if the pod name was generated (or is scheduled for generation), we can begin an investigation into the controlling reference for the pod.

	// heuristic for deployment detection
	if wl.ControllerKind == "ReplicaSet" && strings.HasSuffix(wl.ControllerName, wl.Labels["pod-template-hash"]) {
		name := strings.TrimSuffix(wl.ControllerName, "-"+wl.Labels["pod-template-hash"])
		return name, wmpb.WorkloadMetadataResource_KUBERNETES_DEPLOYMENT
	}

	if wl.ControllerKind == "Job" {
		// figure out how to go from Job -> CronJob
		return wl.ControllerName, wmpb.WorkloadMetadataResource_KUBERNETES_JOB
	}

	if wl.ControllerKind == "CronJob" {
		// figure out how to go from Job -> CronJob
		return wl.ControllerName, wmpb.WorkloadMetadataResource_KUBERNETES_CRONJOB
	}

	return wl.Name, wmpb.WorkloadMetadataResource_KUBERNETES_POD
}
