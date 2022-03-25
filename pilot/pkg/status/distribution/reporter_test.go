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

package distribution

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"k8s.io/utils/clock"

	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/util/sets"
	"istio.io/pkg/ledger"
)

func TestStatusMaps(t *testing.T) {
	r := initReporterWithoutStarting()
	typ := ""
	r.processEvent("conA", typ, "a")
	r.processEvent("conB", typ, "a")
	r.processEvent("conC", typ, "c")
	r.processEvent("conD", typ, "d")
	RegisterTestingT(t)
	x := struct{}{}
	Expect(r.status).To(Equal(map[string]string{"conA~": "a", "conB~": "a", "conC~": "c", "conD~": "d"}))
	Expect(r.reverseStatus).To(Equal(map[string]sets.Set{"a": {"conA~": x, "conB~": x}, "c": {"conC~": x}, "d": {"conD~": x}}))
	r.processEvent("conA", typ, "d")
	Expect(r.status).To(Equal(map[string]string{"conA~": "d", "conB~": "a", "conC~": "c", "conD~": "d"}))
	Expect(r.reverseStatus).To(Equal(map[string]sets.Set{"a": {"conB~": x}, "c": {"conC~": x}, "d": {"conD~": x, "conA~": x}}))
	r.RegisterDisconnect("conA", []xds.EventType{typ})
	Expect(r.status).To(Equal(map[string]string{"conB~": "a", "conC~": "c", "conD~": "d"}))
	Expect(r.reverseStatus).To(Equal(map[string]sets.Set{"a": {"conB~": x}, "c": {"conC~": x}, "d": {"conD~": x}}))
}

func initReporterWithoutStarting() (out Reporter) {
	out.PodName = "tespod"
	out.inProgressResources = map[string]*inProgressEntry{}
	out.client = nil              // TODO
	out.clock = clock.RealClock{} // TODO
	out.UpdateInterval = 300 * time.Millisecond
	out.cm = nil // TODO
	out.reverseStatus = make(map[string]sets.Set)
	out.status = make(map[string]string)
	return
}

func TestBuildReport(t *testing.T) {
	RegisterTestingT(t)
	r := initReporterWithoutStarting()
	r.ledger = ledger.Make(time.Minute)
	resources := []*config.Config{
		{
			Meta: config.Meta{
				Namespace:       "default",
				Name:            "foo",
				ResourceVersion: "1",
			},
		},
		{
			Meta: config.Meta{
				Namespace:       "default",
				Name:            "bar",
				ResourceVersion: "1",
			},
		},
		{
			Meta: config.Meta{
				Namespace:       "alternate",
				Name:            "boo",
				ResourceVersion: "1",
			},
		},
	}
	// cast our model.Configs to Resource because these types aren't compatible
	var myResources []status.Resource
	col := collections.IstioNetworkingV1Alpha3Virtualservices.Resource()
	for _, res := range resources {
		// Set Group Version and GroupVersionKind to real world values from VS
		res.GroupVersionKind = col.GroupVersionKind()
		myResources = append(myResources, status.ResourceFromModelConfig(*res))
		// Add each resource to our ledger for tracking history
		// mark each of our resources as in flight so they are included in the report.
		r.AddInProgressResource(*res)
	}
	firstNoncePrefix := r.ledger.RootHash()
	connections := []string{
		"conA", "conB", "conC",
	}
	// mark each fake connection as having acked version 1 of all resources
	for _, con := range connections {
		r.processEvent(con, "", firstNoncePrefix)
	}
	// modify one resource to version 2
	resources[1].Generation = int64(2)
	myResources[1].Generation = "2"
	// notify the ledger of the new version
	r.AddInProgressResource(*resources[1])
	// mark only one connection as having acked version 2
	r.processEvent(connections[1], "", r.ledger.RootHash())
	// mark one connection as having disconnected.
	r.RegisterDisconnect(connections[2], []xds.EventType{""})
	// build a report, which should have only two dataplanes, with 50% acking v2 of config
	rpt, prunes := r.buildReport()
	r.removeCompletedResource(prunes)
	Expect(rpt.DataPlaneCount).To(Equal(2))
	Expect(rpt.InProgressResources).To(Equal(map[string]int{
		myResources[0].String(): 2,
		myResources[1].String(): 1,
		myResources[2].String(): 2,
	}))
	Expect(r.inProgressResources).NotTo(ContainElement(resources[0]))
}
