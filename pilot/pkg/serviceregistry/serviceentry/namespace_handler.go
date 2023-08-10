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

package serviceentry

import (
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/gvk"
)

// NamespaceDiscoveryHandler is to handle namespace selected or deselected because of discoverySelectors change,
// rather than namespace add/update/delete event triggered from namespace informer.
func (s *Controller) NamespaceDiscoveryHandler(namespace string, event model.Event) {
	if event == model.EventDelete {
		log.Debugf("Handle event namespace %s deselected", namespace)
	} else {
		log.Debugf("Handle event namespace %s selected", namespace)
	}

	cfgs := s.store.List(gvk.WorkloadEntry, namespace)
	for _, cfg := range cfgs {
		s.workloadEntryHandler(cfg, cfg, event)
	}

	if !s.workloadEntryController {
		cfgs := s.store.List(gvk.ServiceEntry, namespace)
		for _, cfg := range cfgs {
			s.serviceEntryHandler(cfg, cfg, event)
		}
	}
}
