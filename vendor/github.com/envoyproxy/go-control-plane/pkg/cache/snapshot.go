// Copyright 2018 Envoyproxy Authors
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.

package cache

import (
	"errors"
	"fmt"
)

// Resources is a versioned group of resources.
type Resources struct {
	// Version information.
	Version string

	// Items in the group.
	Items map[string]Resource
}

// IndexResourcesByName creates a map from the resource name to the resource.
func IndexResourcesByName(items []Resource) map[string]Resource {
	indexed := make(map[string]Resource, len(items))
	for _, item := range items {
		indexed[GetResourceName(item)] = item
	}
	return indexed
}

// NewResources creates a new resource group.
func NewResources(version string, items []Resource) Resources {
	return Resources{
		Version: version,
		Items:   IndexResourcesByName(items),
	}
}

// Snapshot is an internally consistent snapshot of xDS resources.
// Consistentcy is important for the convergence as different resource types
// from the snapshot may be delivered to the proxy in arbitrary order.
type Snapshot struct {
	// Endpoints are items in the EDS response payload.
	Endpoints Resources

	// Clusters are items in the CDS response payload.
	Clusters Resources

	// Routes are items in the RDS response payload.
	Routes Resources

	// Listeners are items in the LDS response payload.
	Listeners Resources

	// Secrets are items in the SDS response payload.
	Secrets Resources
}

// NewSnapshot creates a snapshot from response types and a version.
func NewSnapshot(version string,
	endpoints []Resource,
	clusters []Resource,
	routes []Resource,
	listeners []Resource) Snapshot {
	return Snapshot{
		Endpoints: NewResources(version, endpoints),
		Clusters:  NewResources(version, clusters),
		Routes:    NewResources(version, routes),
		Listeners: NewResources(version, listeners),
	}
}

// Consistent check verifies that the dependent resources are exactly listed in the
// snapshot:
// - all EDS resources are listed by name in CDS resources
// - all RDS resources are listed by name in LDS resources
//
// Note that clusters and listeners are requested without name references, so
// Envoy will accept the snapshot list of clusters as-is even if it does not match
// all references found in xDS.
func (s *Snapshot) Consistent() error {
	if s == nil {
		return errors.New("nil snapshot")
	}
	endpoints := GetResourceReferences(s.Clusters.Items)
	if len(endpoints) != len(s.Endpoints.Items) {
		return fmt.Errorf("mismatched endpoint reference and resource lengths: %v != %d", endpoints, len(s.Endpoints.Items))
	}
	if err := superset(endpoints, s.Endpoints.Items); err != nil {
		return err
	}

	routes := GetResourceReferences(s.Listeners.Items)
	if len(routes) != len(s.Routes.Items) {
		return fmt.Errorf("mismatched route reference and resource lengths: %v != %d", routes, len(s.Routes.Items))
	}
	return superset(routes, s.Routes.Items)
}

// GetResources selects snapshot resources by type.
func (s *Snapshot) GetResources(typ string) map[string]Resource {
	if s == nil {
		return nil
	}
	switch typ {
	case EndpointType:
		return s.Endpoints.Items
	case ClusterType:
		return s.Clusters.Items
	case RouteType:
		return s.Routes.Items
	case ListenerType:
		return s.Listeners.Items
	case SecretType:
		return s.Secrets.Items
	}
	return nil
}

// GetVersion returns the version for a resource type.
func (s *Snapshot) GetVersion(typ string) string {
	if s == nil {
		return ""
	}
	switch typ {
	case EndpointType:
		return s.Endpoints.Version
	case ClusterType:
		return s.Clusters.Version
	case RouteType:
		return s.Routes.Version
	case ListenerType:
		return s.Listeners.Version
	case SecretType:
		return s.Secrets.Version
	}
	return ""
}
