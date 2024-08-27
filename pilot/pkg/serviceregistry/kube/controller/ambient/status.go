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
	"fmt"

	"istio.io/istio/pilot/pkg/model"
)

func ReportWaypointIsNotReady(waypoint string) *model.StatusMessage {
	return &model.StatusMessage{
		Reason:  "WaypointIsNotReady",
		Message: fmt.Sprintf("waypoint %q is not ready", waypoint),
	}
}

func ReportWaypointAttachmentDenied(waypoint string) *model.StatusMessage {
	return &model.StatusMessage{
		Reason:  "AttachmentDenied",
		Message: fmt.Sprintf("we are not permitted to attach to waypoint %q (missing allowedRoutes?)", waypoint),
	}
}

func ReportWaypointUnsupportedTrafficType(waypoint string, ttype string) *model.StatusMessage {
	return &model.StatusMessage{
		Reason:  "UnsupportedTrafficType",
		Message: fmt.Sprintf("attempting to bind to traffic type %q which the waypoint %q does not support", ttype, waypoint),
	}
}
