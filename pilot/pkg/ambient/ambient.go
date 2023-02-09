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

package ambient

import (
	"time"

	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"

	"istio.io/istio/pkg/spiffe"
)

type WorkloadMetadata struct {
	Containers     []string
	GenerateName   string
	ControllerName string
	ControllerKind string
}

// TODO shouldn't call this a workload. Maybe "node" encapsulates all of ztunnel, waypoints, workload
type Workload struct {
	UID         string
	Name        string
	Namespace   string
	Annotations map[string]string
	Labels      map[string]string

	ServiceAccount string
	NodeName       string
	PodIP          string
	PodIPs         []string
	HostNetwork    bool

	WorkloadMetadata

	CreationTimestamp time.Time
}

// Identity generates SecureNamingSAN but for Workload instead of Pod
func (w Workload) Identity() string {
	return spiffe.MustGenSpiffeURI(w.Namespace, w.ServiceAccount)
}

func (w Workload) Equals(w2 Workload) bool {
	if w.UID != w2.UID {
		return false
	}
	if w.Name != w2.Name {
		return false
	}
	if w.Namespace != w2.Namespace {
		return false
	}

	if w.ServiceAccount != w2.ServiceAccount {
		return false
	}
	if w.NodeName != w2.NodeName {
		return false
	}
	if w.PodIP != w2.PodIP {
		return false
	}
	if !w.CreationTimestamp.Equal(w2.CreationTimestamp) {
		return false
	}
	if !maps.Equal(w.Labels, w2.Labels) {
		return false
	}
	if !maps.Equal(w.Annotations, w2.Annotations) {
		return false
	}
	if !slices.Equal(w.PodIPs, w2.PodIPs) {
		return false
	}
	if !slices.Equal(w.WorkloadMetadata.Containers, w2.WorkloadMetadata.Containers) {
		return false
	}
	if w.WorkloadMetadata.GenerateName != w2.WorkloadMetadata.GenerateName {
		return false
	}
	if w.WorkloadMetadata.ControllerName != w2.WorkloadMetadata.ControllerName {
		return false
	}
	if w.WorkloadMetadata.ControllerKind != w2.WorkloadMetadata.ControllerKind {
		return false
	}

	return true
}

type NodeType = string

const (
	LabelStatus = "istio.io/ambient-status"
	TypeEnabled = "enabled"
	// LabelType == "workload" -> intercept into ztunnel
	// TODO this could be an annotation – eventually move it into api repo
	LabelType = "ambient-type"

	TypeWorkload NodeType = "workload"
	TypeNone     NodeType = "none"
	TypeWaypoint NodeType = "waypoint"
)
