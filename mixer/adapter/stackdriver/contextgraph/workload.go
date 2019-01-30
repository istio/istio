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

package contextgraph

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"istio.io/istio/mixer/pkg/adapter"
)

const membershipTypeName = "google.cloud.contextgraph.Membership"

type workloadInstance struct {
	// N.B. The projects can potentially be different for each workload.
	meshUID                                      string
	istioProject                                 string
	clusterProject, clusterLocation, clusterName string
	uid                                          string
	owner                                        string
	workloadName, workloadNamespace              string
}

type service struct {
	meshUID      string
	namespace    string
	name         string
	istioProject string
}

// Reify turns wi into a set of Context API entities and edges.
func (wi workloadInstance) Reify(logger adapter.Logger) ([]entity, []edge) {
	gcpContainer := fmt.Sprintf("//cloudresourcemanager.googleapis.com/projects/%s", wi.istioProject)
	// N.B. Project names can contain ":" which needs to /not/ be escaped.
	istioContainer := fmt.Sprintf("//istio.io/projects/%s", wi.istioProject)
	meshUID := url.QueryEscape(wi.meshUID)
	clusterProject := wi.clusterProject
	// TODO: Figure out if locations should be URL-escaped or not ("aws:us-east-1" is a valid region).
	clusterLocation := url.QueryEscape(wi.clusterLocation)
	clusterName := url.QueryEscape(wi.clusterName)
	uid := url.QueryEscape(wi.uid)
	ownerUID := url.QueryEscape(wi.owner)
	workloadName := url.QueryEscape(wi.workloadName)
	workloadNamespace := url.QueryEscape(wi.workloadNamespace)
	wiFullName := fmt.Sprintf(
		"%s/meshes/%s/clusterProjects/%s/locations/%s/clusters/%s/workloadInstances/%s",
		istioContainer, meshUID, clusterProject, clusterLocation, clusterName, uid,
	)
	wiEntity := entity{
		gcpContainer,
		"io.istio.WorkloadInstance",
		wiFullName,
		clusterLocation,
		[4]string{meshUID, clusterProject, clusterName, uid},
	}
	ownerFullName := fmt.Sprintf(
		"%s/meshes/%s/clusterProjects/%s/locations/%s/clusters/%s/owners/%s",
		istioContainer, meshUID, clusterProject, clusterLocation, clusterName, ownerUID,
	)
	owner := entity{
		gcpContainer,
		"io.istio.Owner",
		ownerFullName,
		clusterLocation,
		[4]string{meshUID, clusterProject, clusterName, ownerUID},
	}
	workloadFullName := fmt.Sprintf(
		"%s/meshes/%s/workloads/%s/%s",
		istioContainer, meshUID, workloadNamespace, workloadName,
	)
	workload := entity{
		gcpContainer,
		"io.istio.Workload",
		workloadFullName,
		"global",
		[4]string{meshUID, workloadNamespace, workloadName, ""},
	}
	clusterContainer := clusterContainer(wi.clusterProject, wi.clusterLocation, wi.clusterName)

	var ownerK8sFullName string
	t := strings.Split(wi.owner, "/")
	if len(t) >= 3 && t[0] == "kubernetes:" {
		var name, namespace, typeName string
		t = t[2:]
		switch {
		case len(t) >= 6 && t[0] == "apis" && t[1] == "v1":
			// pods, RC
			namespace = t[3]
			name = t[5]
			typeName = t[4]
		case len(t) >= 7 && t[0] == "apis" && (t[1] == "extensions" || t[1] == "apps" || t[1] == "batch"):
			// cronjobs, jobs, daemonsets, deployments, replicasets, statefulsets
			namespace = t[4]
			name = t[6]
			typeName = t[1] + "/" + t[5]
		}
		if name != "" {
			ownerK8sFullName = fmt.Sprintf("%s/k8s/namespaces/%s/%s/%s",
				clusterContainer, namespace, typeName, name)
		} else {
			logger.Warningf("Couldn't parse owner into k8s obj: %s", wi.owner)
		}
	} else {
		if wi.owner != "unknown" {
			logger.Warningf("Unknown owner type: %s", wi.owner)
		}
	}
	// TODO: Non-k8s owners.

	// "kubernetes://istio-pilot-65d79b966c-xnbx8.istio-system"
	var wiK8sFullName string
	t = strings.Split(wi.uid, "/")
	if len(t) == 3 && t[0] == "kubernetes:" {
		nameNs := strings.Split(t[2], ".")
		if len(nameNs) == 2 {
			namespace, name := nameNs[1], nameNs[0]
			wiK8sFullName = fmt.Sprintf("%s/k8s/namespaces/%s/pods/%s", clusterContainer, namespace, name)
		} else {
			logger.Warningf("Unknown workload instance type: %s", wi.uid)
		}
	} else {
		if wi.uid != "Unknown" {
			logger.Warningf("Unknown workload instance type: %s", wi.uid)
		}
	}
	// TODO: Non-k8s workload instances

	edges := []edge{
		{workloadFullName, ownerFullName, membershipTypeName},
		{ownerFullName, wiFullName, membershipTypeName},
	}
	if ownerK8sFullName != "" {
		edges = append(edges, edge{ownerFullName, ownerK8sFullName, membershipTypeName})
	}
	if wiK8sFullName != "" {
		edges = append(edges, edge{wiFullName, wiK8sFullName, membershipTypeName})
	}
	return []entity{wiEntity, owner, workload}, edges
}

func (s service) Reify() entity {
	return entity{
		containerFullName: fmt.Sprintf("//cloudresourcemanager.googleapis.com/projects/%s",
			s.istioProject),
		typeName: "io.istio.Service",
		fullName: fmt.Sprintf("//istio.io/projects/%s/meshes/%s/services/%s/%s",
			s.istioProject,
			url.QueryEscape(s.meshUID),
			url.QueryEscape(s.namespace),
			url.QueryEscape(s.name)),
		location: "global",
		shortNames: [4]string{
			url.QueryEscape(s.meshUID),
			url.QueryEscape(s.namespace),
			url.QueryEscape(s.name),
			"",
		},
	}
}

type trafficAssertion struct {
	source, destination          workloadInstance
	contextProtocol, apiProtocol string
	destinationService           service
	timestamp                    time.Time
}

func (t trafficAssertion) Reify(logger adapter.Logger) ([]entity, []edge) {
	var sourceFullNames, destinationFullNames []string
	var srcInst, srcOwn, dstInst, dstOwn, dstCluster, dstProject, dstLoc string
	entities, edges := t.source.Reify(logger)
	for _, entity := range entities {
		sourceFullNames = append(sourceFullNames, entity.fullName)
		switch entity.typeName {
		case "io.istio.WorkloadInstance":
			srcInst = entity.fullName
			dstLoc = entity.location
			dstCluster = entity.shortNames[2] // todo: make this less fragile
			dstProject = entity.shortNames[1]
		case "io.istio.Owner":
			srcOwn = entity.fullName
		}
	}
	destEntities, destEdges := t.destination.Reify(logger)
	for _, entity := range destEntities {
		entities = append(entities, entity)
		destinationFullNames = append(destinationFullNames, entity.fullName)
		switch entity.typeName {
		case "io.istio.WorkloadInstance":
			dstInst = entity.fullName
		case "io.istio.Owner":
			dstOwn = entity.fullName
		}
	}
	edges = append(edges, destEdges...)
	serviceEntity := t.destinationService.Reify()
	entities = append(entities, serviceEntity)

	var typeName string
	var protocol string
	switch t.contextProtocol {
	case "tcp", "http", "grpc":
		protocol = t.contextProtocol
	default:
		protocol = t.apiProtocol
	}
	switch protocol {
	case "http":
		typeName = "google.cloud.contextgraph.Communication.Http"
	case "tcp":
		typeName = "google.cloud.contextgraph.Communication.Tcp"
	case "https":
		typeName = "google.cloud.contextgraph.Communication.Https"
	case "grpc":
		typeName = "google.cloud.contextgraph.Communication.Grpc"
	default:
		logger.Warningf("Unknown type of protocol: %s", protocol)
	}
	if typeName != "" {
		// Publish the full N-way relationships.
		for _, s := range sourceFullNames {
			for _, d := range destinationFullNames {
				edges = append(edges, edge{s, d, typeName})
			}
		}
		edges = append(edges, edge{srcInst, serviceEntity.fullName, typeName})
		edges = append(edges, edge{srcOwn, serviceEntity.fullName, typeName})
		edges = append(edges, edge{serviceEntity.fullName, dstInst, typeName})
		edges = append(edges, edge{serviceEntity.fullName, dstOwn, typeName})

		k8sSvc := k8sSvcFullname(dstProject, dstLoc, dstCluster, t.destinationService.namespace, t.destinationService.name)
		edges = append(edges, edge{serviceEntity.fullName, k8sSvc, membershipTypeName})
	}

	return entities, edges
}

// example: //container.googleapis.com/projects/<project>/locations/us-central1-a/clusters/<cluster>/k8s/namespaces/default/services/<service>
func k8sSvcFullname(project, location, cluster, namespace, name string) string {
	return fmt.Sprintf("%s/k8s/namespaces/%s/services/%s", clusterContainer(project, location, cluster), namespace, name)
}

func clusterContainer(project, location, cluster string) string {
	// TODO: Figure out what the container is for non-GCE clusters.
	locType := "locations"
	if strings.Count(location, "-") == 2 {
		locType = "zones"
	}
	return fmt.Sprintf("//container.googleapis.com/projects/%s/%s/%s/clusters/%s", project, locType, location, cluster)
}

type entity struct {
	containerFullName string
	typeName          string
	fullName          string
	location          string
	// N.B. map keys can only have arrays, not slices.
	// 4 is enough for all our entity types.
	shortNames [4]string
}

type edge struct {
	sourceFullName, destinationFullName string
	typeName                            string
}
