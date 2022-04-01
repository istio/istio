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

package xds

import (
	"fmt"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	"istio.io/istio/pilot/pkg/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/util/sets"
)

var knownOptimizationGaps = sets.New(
	"BlackHoleCluster",
	"InboundPassthroughClusterIpv4",
	"InboundPassthroughClusterIpv6",
	"PassthroughCluster",
)

// compareDiff compares a Delta and SotW XDS response. This allows checking that the Delta XDS
// response returned the optimal result. Checks include correctness checks (e.g. if a config changed,
// we must include it) and possible optimizations (e.g. we sent a config, but it was not changed).
func (s *DiscoveryServer) compareDiff(
	con *Connection,
	w *model.WatchedResource,
	full model.Resources,
	resp model.Resources,
	deleted model.DeletedResources,
	usedDelta bool,
	delta model.ResourceDelta,
) {
	current := con.Watched(w.TypeUrl).LastResources
	if current == nil {
		log.Debugf("ADS:%s: resources initialized", v3.GetShortType(w.TypeUrl))
		return
	}
	if resp == nil && deleted == nil && len(full) == 0 {
		// TODO: it suspicious full is never nil - are there case where we should be deleting everything?
		// Both SotW and Delta did not respond, nothing to compare
		return
	}
	newByName := map[string]*discovery.Resource{}
	for _, v := range full {
		newByName[v.Name] = v
	}
	curByName := map[string]*discovery.Resource{}
	for _, v := range current {
		curByName[v.Name] = v
	}

	watched := sets.New(w.ResourceNames...)

	details := fmt.Sprintf("last:%v sotw:%v delta:%v-%v", len(current), len(full), len(resp), len(deleted))
	wantDeleted := sets.New()
	wantChanged := sets.New()
	wantUnchanged := sets.New()
	for _, c := range current {
		n := newByName[c.Name]
		if n == nil {
			// We had a resource, but SotW didn't generate it.
			if watched.Contains(c.Name) {
				// We only need to delete it if Envoy is watching. Otherwise, it may have simply unsubscribed
				wantDeleted.Insert(c.Name)
			}
		} else if diff := cmp.Diff(c.Resource, n.Resource, protocmp.Transform()); diff != "" {
			// Resource was modified
			wantChanged.Insert(c.Name)
		} else {
			// No diff. Ideally delta doesn't send any update here
			wantUnchanged.Insert(c.Name)
		}
	}
	for _, v := range full {
		if _, f := curByName[v.Name]; !f {
			// Resource is added. Delta doesn't distinguish add vs update, so just put it with changed
			wantChanged.Insert(v.Name)
		}
	}

	gotDeleted := sets.New()
	if usedDelta {
		gotDeleted.InsertAll(deleted...)
	}
	gotChanged := sets.New()
	for _, v := range resp {
		gotChanged.Insert(v.Name)
	}

	// BUGS
	extraDeletes := gotDeleted.Difference(wantDeleted).SortedList()
	missedDeletes := wantDeleted.Difference(gotDeleted).SortedList()
	missedChanges := wantChanged.Difference(gotChanged).SortedList()

	// Optimization Potential
	extraChanges := gotChanged.Difference(wantChanged).Difference(knownOptimizationGaps).SortedList()
	if len(delta.Subscribed) > 0 {
		// Delta is configured to build only the request resources. Make sense we didn't build anything extra
		if !wantChanged.SupersetOf(gotChanged) {
			log.Errorf("%s: TEST for node:%s unexpected resources: %v %v", v3.GetShortType(w.TypeUrl), con.proxy.ID, details, wantChanged.Difference(gotChanged))
		}
		// Still make sure we didn't delete anything extra
		if len(extraDeletes) > 0 {
			log.Errorf("%s: TEST for node:%s unexpected deletions: %v %v", v3.GetShortType(w.TypeUrl), con.proxy.ID, details, extraDeletes)
		}
	} else {
		if len(extraDeletes) > 0 {
			log.Errorf("%s: TEST for node:%s unexpected deletions: %v %v", v3.GetShortType(w.TypeUrl), con.proxy.ID, details, extraDeletes)
		}
		if len(missedDeletes) > 0 {
			log.Errorf("%s: TEST for node:%s missed deletions: %v %v", v3.GetShortType(w.TypeUrl), con.proxy.ID, details, missedDeletes)
		}
		if len(missedChanges) > 0 {
			log.Errorf("%s: TEST for node:%s missed changes: %v %v", v3.GetShortType(w.TypeUrl), con.proxy.ID, details, missedChanges)
		}
		if len(extraChanges) > 0 {
			if usedDelta {
				log.Infof("%s: TEST for node:%s missed possible optimization: %v. deleted:%v changed:%v",
					v3.GetShortType(w.TypeUrl), con.proxy.ID, extraChanges, len(gotDeleted), len(gotChanged))
			} else {
				log.Debugf("%s: TEST for node:%s missed possible optimization: %v. deleted:%v changed:%v",
					v3.GetShortType(w.TypeUrl), con.proxy.ID, extraChanges, len(gotDeleted), len(gotChanged))
			}
		}
	}
}

func applyDelta(message model.Resources, resp *discovery.DeltaDiscoveryResponse) model.Resources {
	deleted := sets.New(resp.RemovedResources...)
	byName := map[string]*discovery.Resource{}
	for _, v := range resp.Resources {
		byName[v.Name] = v
	}
	res := model.Resources{}
	for _, m := range message {
		if deleted.Contains(m.Name) {
			continue
		}
		if replaced := byName[m.Name]; replaced != nil {
			res = append(res, replaced)
			delete(byName, m.Name)
			continue
		}
		res = append(res, m)
	}
	for _, v := range byName {
		res = append(res, v)
	}
	return res
}
