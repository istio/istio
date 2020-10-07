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
	"github.com/gogo/protobuf/types"
	"istio.io/api/meta/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/gvk"
	"strings"
)

type HealthEvent struct {
	Healthy bool
	Message string
}

func (sg *InternalGen) UpdateWorkloadStatus(proxy *model.Proxy, event HealthEvent) {

	if len(proxy.Metadata.Namespace) == 0 {
		adsLog.Errorf("workload update of %v failed: missing namespace", proxy.ID)
		return
	}

	// todo change when auto-registration actually happens
	name := getWorkloadEntryName(proxy.IPAddresses[0])
	// get the current workload status
	entryCfg := sg.Store.Get(gvk.WorkloadEntry, name, proxy.Metadata.Namespace)
	if entryCfg == nil {
		adsLog.Errorf("attempted update of workload %v failed: doesn't exist", proxy.ID)
	}
	// initialize the status if it doesnt exist. Ideally this doesn't happen because we
	// should never create an object with a nil status.
	if entryCfg.Status == nil {
		entryCfg.Status = &v1alpha1.IstioStatus{
			Conditions: []*v1alpha1.IstioCondition{},
		}
	}
	stat := entryCfg.Status.(*v1alpha1.IstioStatus)
	cond := &v1alpha1.IstioCondition{
		Type: "Health",
		// last probe time will be now
		LastProbeTime: types.TimestampNow(),
		// last transition time will also be now because health events are only sent on
		// transitions from healthy to unhealthy or vice versa. we do not need to track
		// the last transition.
		LastTransitionTime: types.TimestampNow(),
	}

	if event.Healthy {
		cond.Status = "True"
	} else {
		cond.Status = "False"
		cond.Message = event.Message
	}

	// append the condition
	stat.Conditions = append(stat.Conditions, cond)

	// todo update status in store

}

// this assumes this discovery request is a health event
func BuildHealthEvent(req *discovery.DiscoveryRequest) HealthEvent {
	ev := HealthEvent{}
	if req.ErrorDetail != nil {
		ev.Healthy = false
		ev.Message = fmt.Sprintf("code: %v, message: %v", req.ErrorDetail.Code, req.ErrorDetail.Message)
	} else {
		ev.Healthy = true
	}
	return ev
}

func getWorkloadEntryName(address string) string {
	return fmt.Sprintf("auto-%s", strings.ReplaceAll(address, ".", "-"))
}
