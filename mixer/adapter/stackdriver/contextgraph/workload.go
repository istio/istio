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

// Reify turns wi into a set of Context API entities and edges.
func (wi workloadInstance) Reify(logger adapter.Logger) ([]entity, []edge) {
	gcpContainer := fmt.Sprintf("//cloudresourcemanager.googleapis.com/projects/%s", wi.istioProject)
	istioContainer := fmt.Sprintf("//istio.io/projects/%s", wi.istioProject)
	meshUID := url.QueryEscape(wi.meshUID)
	clusterProject := url.QueryEscape(wi.clusterProject)
	clusterLocation := url.QueryEscape(wi.clusterLocation)
	clusterName := url.QueryEscape(wi.clusterName)
	uid := url.QueryEscape(wi.uid)
	ownerUid := url.QueryEscape(wi.owner)
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
		istioContainer, meshUID, clusterProject, clusterLocation, clusterName, ownerUid,
	)
	owner := entity{
		gcpContainer,
		"io.istio.Owner",
		ownerFullName,
		clusterLocation,
		[4]string{meshUID, clusterProject, clusterName, ownerUid},
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
	// TODO: Figure out what the container is for non-GCE clusters.
	clusterContainer := fmt.Sprintf("//container.googleapis.com/projects/%s/zones/%s/clusters/%s", wi.clusterProject, wi.clusterLocation, wi.clusterName)

	var ownerK8sFullName string
	t := strings.Split(wi.owner, "/")
	if len(t) >= 3 && t[0] == "kubernetes:" {
		var name, namespace, typeName string
		t = t[2:len(t)]
		switch {
		case len(t) >= 6 && t[0] == "apis" && t[1] == "v1": // pods, replicationcontrollers
			namespace = t[3]
			name = t[5]
			typeName = t[4]
		case len(t) >= 7 && t[0] == "apis" && (t[1] == "extensions" || t[1] == "apps" || t[1] == "batch"): // cronjobs, jobs, daemonsets, deployments, replicasets, statefulsets
			namespace = t[4]
			name = t[6]
			typeName = t[1] + "/" + t[5]
		}
		if name != "" {
			ownerK8sFullName = fmt.Sprintf("%s/k8s/namespaces/%s/%s/%s", clusterContainer, namespace, typeName, name)
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
		name_ns := strings.Split(t[2], ".")
		if len(name_ns) == 2 {
			namespace, name := name_ns[1], name_ns[0]
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

type trafficAssertion struct {
	source, destination          workloadInstance
	contextProtocol, apiProtocol string
	timestamp                    time.Time
}

func (t trafficAssertion) Reify(logger adapter.Logger) ([]entity, []edge) {
	var sourceFullNames, destinationFullNames []string
	entities, edges := t.source.Reify(logger)
	for _, entity := range entities {
		sourceFullNames = append(sourceFullNames, entity.fullName)
	}
	destEntities, destEdges := t.destination.Reify(logger)
	for _, entity := range destEntities {
		entities = append(entities, entity)
		destinationFullNames = append(destinationFullNames, entity.fullName)
	}
	edges = append(edges, destEdges...)

	var typeName string
	var protocol string
	switch t.contextProtocol {
	case "tcp", "http":
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
	}
	return entities, edges
}

type edge struct {
	sourceFullName, destinationFullName string
	typeName                            string
}

// entityCache tracks the entities we've already seen.
// It is not thread-safe.
type entityCache struct {
	// cache maps an entity to the epoch it was last seen in.
	cache     map[entity]int
	lastFlush int
	logger    adapter.Logger
}

func newEntityCache(logger adapter.Logger) *entityCache {
	return &entityCache{
		cache:  make(map[entity]int),
		logger: logger,
	}
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

// AssertAndCheck reports the existence of e at epoch, and returns true if the entity needs to be sent immediately.
func (ec *entityCache) AssertAndCheck(e entity, epoch int) bool {
	cEpoch, ok := ec.cache[e]
	ec.cache[e] = epoch
	if !ok || cEpoch < ec.lastFlush {
		ec.logger.Debugf("%q needs to be sent anew, old epoch: %d, now seen: %d", e.fullName, cEpoch, epoch)
		return true
	}
	return false
}

// Flush returns the list of entities that have been asserted in the most recent epoch, to be reasserted.
// It also cleans up stale entries from the cache.
func (ec *entityCache) Flush(epoch int) []entity {
	var result []entity
	for k, e := range ec.cache {
		if e < ec.lastFlush {
			delete(ec.cache, k)
			continue
		}
		if e == epoch {
			// Don't republish entities that are already in this batch.
			continue
		}
		result = append(result, k)
	}
	ec.lastFlush = epoch
	return result
}

// edgeCache tracks the edges we've already seen.
// It is not thread-safe.
type edgeCache struct {
	// cache maps an edge to the epoch it was last seen in.
	cache     map[edge]int
	lastFlush int
	logger    adapter.Logger
}

func newEdgeCache(logger adapter.Logger) *edgeCache {
	return &edgeCache{
		cache:  make(map[edge]int),
		logger: logger,
	}
}

// AssertAndCheck reports the existence of e at epoch, and returns true if the edge needs to be sent immediately.
func (ec *edgeCache) AssertAndCheck(e edge, epoch int) bool {
	cEpoch, ok := ec.cache[e]
	ec.cache[e] = epoch
	if !ok || cEpoch < ec.lastFlush {
		ec.logger.Debugf("%v needs to be sent anew, old epoch: %d, now seen: %d", e, cEpoch, epoch)
		return true
	}
	return false
}

// Flush returns the list of entities that have been asserted since the last flush, to be reasserted.
// It also cleans up stale entries from the cache.
func (ec *edgeCache) Flush(epoch int) []edge {
	var result []edge
	for k, e := range ec.cache {
		if e < ec.lastFlush {
			delete(ec.cache, k)
			continue
		}
		if e == epoch {
			// Don't republish edges that are already in this batch.
			continue
		}
		result = append(result, k)
	}
	ec.lastFlush = epoch
	return result
}

// Invalidate removes all edges with a source of fullName from the cache, so the next assertion will trigger a report.
func (ec *edgeCache) Invalidate(fullName string) {
	for e := range ec.cache {
		if e.sourceFullName == fullName {
			delete(ec.cache, e)
		}
	}
}
