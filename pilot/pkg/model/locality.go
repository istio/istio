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

package model

// LocalityInterface is wrapper for both Locality and Envoy core.Locality.
// It is used by LocalityMatch to support the two Localities simultaneously.
type LocalityInterface interface {
	GetRegion() string
	GetZone() string
	GetSubZone() string
}

// Identifies location of where either Envoy runs or where upstream hosts run.
type Locality struct {
	// Region this proxy belongs to.
	Region string
	// Defines the local service zone where Envoy is running. Though optional, it
	// should be set if discovery service routing is used and the discovery
	// service exposes :ref:`zone data <envoy_api_field_endpoint.LocalityLbEndpoints.locality>`,
	// either in this message or via :option:`--service-zone`. The meaning of zone
	// is context dependent, e.g. `Availability Zone (AZ)
	// <https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-regions-availability-zones.html>`_
	// on AWS, `Zone <https://cloud.google.com/compute/docs/regions-zones/>`_ on
	// GCP, etc.
	Zone string
	// When used for locality of upstream hosts, this field further splits zone
	// into smaller chunks of sub-zones so they can be load balanced
	// independently.
	SubZone string
}

func (l *Locality) GetRegion() string {
	if l != nil {
		return l.Region
	}
	return ""
}

func (l *Locality) GetZone() string {
	if l != nil {
		return l.Zone
	}
	return ""
}

func (l *Locality) GetSubZone() string {
	if l != nil {
		return l.SubZone
	}
	return ""
}
