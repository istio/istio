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

func getRouteDestinations(vs *v1alpha3.VirtualService) []*v1alpha3.Destination {
	destinations := make([]*v1alpha3.Destination, 0)

	for _, r := range vs.GetTcp() {
		for _, rd := range r.GetRoute() {
			destinations = append(destinations, rd.GetDestination())
		}
	}
	for _, r := range vs.GetTls() {
		for _, rd := range r.GetRoute() {
			destinations = append(destinations, rd.GetDestination())
		}
	}
	for _, r := range vs.GetHttp() {
		for _, rd := range r.GetRoute() {
			destinations = append(destinations, rd.GetDestination())
		}
	}

	return destinations
}

func getHTTPMirrorDestinations(vs *v1alpha3.VirtualService) []*v1alpha3.Destination {
	var destinations []*v1alpha3.Destination

	for _, r := range vs.GetHttp() {
		if m := r.GetMirror(); m != nil {
			destinations = append(destinations, m)
		}
	}

	return destinations
}
