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
package virtualservice

import (
	"istio.io/api/networking/v1alpha3"
)

// Destination is created because the index of the destination along with the destination object are needed
// for calculating the destination path in the YAML resource
type Destination struct {
	routeRule        string
	serviceIndex     int
	destinationIndex int
	destination      *v1alpha3.Destination
}

func (d *Destination) GetRouteRule() string {
	return d.routeRule
}

func (d *Destination) GetServiceIndex() int {
	return d.serviceIndex
}

func (d *Destination) GetDestinationIndex() int {
	return d.destinationIndex
}

func (d *Destination) GetDestination() *v1alpha3.Destination {
	return d.destination
}

func getRouteDestinations(vs *v1alpha3.VirtualService) []*Destination {
	destinations := make([]*Destination, 0)
	for i, r := range vs.GetTcp() {
		for j, rd := range r.GetRoute() {
			destinations = append(destinations, &Destination{
				routeRule:        "tcp",
				serviceIndex:     i,
				destinationIndex: j,
				destination:      rd.GetDestination(),
			})
		}
	}
	for i, r := range vs.GetTls() {
		for j, rd := range r.GetRoute() {
			destinations = append(destinations, &Destination{
				routeRule:        "tls",
				serviceIndex:     i,
				destinationIndex: j,
				destination:      rd.GetDestination(),
			})
		}
	}
	for i, r := range vs.GetHttp() {
		for j, rd := range r.GetRoute() {
			destinations = append(destinations, &Destination{
				routeRule:        "http",
				serviceIndex:     i,
				destinationIndex: j,
				destination:      rd.GetDestination(),
			})
		}
	}

	return destinations
}

func getHTTPMirrorDestinations(vs *v1alpha3.VirtualService) []*Destination {
	var destinations []*Destination

	for i, r := range vs.GetHttp() {
		if m := r.GetMirror(); m != nil {
			destinations = append(destinations, &Destination{
				serviceIndex: i,
				destination:  m,
			})
		}
	}

	return destinations
}
