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

package status

import (
	"testing"
	"time"

	"istio.io/pkg/ledger"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/config/schema/collections"

	. "github.com/onsi/gomega"
	"k8s.io/utils/clock"
)

func TestStatusMaps(t *testing.T) {
	r := initReporterWithoutStarting()
	typ := xds.UnknownEventType
	r.processEvent("conA", typ, "a")
	r.processEvent("conB", typ, "a")
	r.processEvent("conC", typ, "c")
	r.processEvent("conD", typ, "d")
	RegisterTestingT(t)
	x := struct{}{}
	Expect(r.status).To(Equal(map[string]string{"conA": "a", "conB": "a", "conC": "c", "conD": "d"}))
	Expect(r.reverseStatus).To(Equal(map[string]map[string]struct{}{"a": {"conA": x, "conB": x}, "c": {"conC": x}, "d": {"conD": x}}))
	r.processEvent("conA", typ, "d")
	Expect(r.status).To(Equal(map[string]string{"conA": "d", "conB": "a", "conC": "c", "conD": "d"}))
	Expect(r.reverseStatus).To(Equal(map[string]map[string]struct{}{"a": {"conB": x}, "c": {"conC": x}, "d": {"conD": x, "conA": x}}))
	r.RegisterDisconnect("conA", []xds.EventType{typ})
	Expect(r.status).To(Equal(map[string]string{"conB": "a", "conC": "c", "conD": "d"}))
	Expect(r.reverseStatus).To(Equal(map[string]map[string]struct{}{"a": {"conB": x}, "c": {"conC": x}, "d": {"conD": x}}))
}

func initReporterWithoutStarting() (out Reporter) {
	out.PodName = "tespod"
	out.inProgressResources = map[string]*inProgressEntry{}
	out.client = nil              // TODO
	out.clock = clock.RealClock{} // TODO
	out.UpdateInterval = 300 * time.Millisecond
	out.store = nil // TODO
	out.cm = nil    // TODO
	out.reverseStatus = make(map[string]map[string]struct{})
	out.status = make(map[string]string)
	return
}

func TestBuildReport(t *testing.T) {
	RegisterTestingT(t)
	r := initReporterWithoutStarting()
	r.store = memory.Make(collections.All)
	l := ledger.Make(time.Minute)
	resources := []*model.Config{
		{
			ConfigMeta: model.ConfigMeta{
				Namespace:       "default",
				Name:            "foo",
				ResourceVersion: "1",
			},
		},
		{
			ConfigMeta: model.ConfigMeta{
				Namespace:       "default",
				Name:            "bar",
				ResourceVersion: "1",
			},
		},
		{
			ConfigMeta: model.ConfigMeta{
				Namespace:       "alternate",
				Name:            "boo",
				ResourceVersion: "1",
			},
		},
	}
	// cast our model.Configs to Resource because these types aren't compatible
	var myResources []Resource
	col := collections.IstioNetworkingV1Alpha3Virtualservices.Resource()
	for _, res := range resources {
		// Set Group Version and GroupVersionKind to real world values from VS
		res.GroupVersionKind = col.GroupVersionKind()
		resStr := res.Key()
		myResources = append(myResources, *ResourceFromModelConfig(*res))
		// Add each resource to our ledger for tracking history
		_, err := l.Put(resStr, res.ResourceVersion)
		// mark each of our resources as in flight so they are included in the report.
		r.AddInProgressResource(*res)
		Expect(err).NotTo(HaveOccurred())
	}
	firstNoncePrefix := l.RootHash()
	connections := []string{
		"conA", "conB", "conC",
	}
	// mark each fake connection as having acked version 1 of all resources
	for _, con := range connections {
		r.processEvent(con, xds.UnknownEventType, firstNoncePrefix)
	}
	// modify one resource to version 2
	resources[1].ResourceVersion = "2"
	myResources[1].ResourceVersion = "2"
	// notify the ledger of the new version
	_, err := l.Put(resources[1].Key(), "2")
	r.AddInProgressResource(*resources[1])
	Expect(err).NotTo(HaveOccurred())
	// mark only one connection as having acked version 2
	r.processEvent(connections[1], "", l.RootHash())
	// mark one connection as having disconnected.
	r.RegisterDisconnect(connections[2], []xds.EventType{xds.UnknownEventType})
	err = r.store.SetLedger(l)
	Expect(err).NotTo(HaveOccurred())
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
