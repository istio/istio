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
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"k8s.io/apimachinery/pkg/api/errors"
	"strconv"
	"strings"
	"time"
)

const (
	// AutoRegistrationGroupAnnotation on a WorkloadEntry stores the associated WorkloadGroup.
	AutoRegistrationGroupAnnotation = "istio.io/autoRegistrationGroup"
	// WorkloadControllerAnnotation on a WorkloadEntry should store the current/last pilot instance connected to the workload for XDS.
	WorkloadControllerAnnotation = "istio.io/workloadController"
	// ConnectedAtAnnotation on a WorkloadEntry stores the time in nanoseconds when the associated workload connected to a Pilot instance.
	ConnectedAtAnnotation = "istio.io/connectedAt"
	// DisconnectedAtAnnotation on a WorkloadEntry stores the time in nanoseconds when the associated workload disconnected from a Pilot instance.
	DisconnectedAtAnnotation = "istio.io/disconnectedAt"
)

func (sg *InternalGen) RegisterWorkload(proxy *model.Proxy, con *Connection) {
	// check if the WE already exists, update the status
	entryName := autoregisteredWorkloadEntryName(proxy)
	if entryName == "" {
		return
	}
	cachedEntry := sg.Store.Get(gvk.WorkloadEntry, entryName, proxy.Metadata.Namespace)
	if cachedEntry != nil {
		entry := cachedEntry.DeepCopy()
		// copy it so we don't modify the cache
		setConnectMeta(&entry, sg.Server.instanceID, con)
		_, err := sg.Store.Update(entry)
		if err != nil {
			adsLog.Warnf("err updating %s with new instance id %s; res ver %s; cached ver; %v", entry.Name, sg.Server.instanceID, entry.ResourceVersion, cachedEntry.ResourceVersion, err)
		}
		if err == nil || !errors.IsNotFound(err) {
			// stop here for success or other errors besides "not-found"
			return
		}
		// fallthrough to the WE creation if it was deleted between the above Get/Update
	}

	// create WorkloadEntry from WorkloadGroup
	groupCfg := sg.Store.Get(gvk.WorkloadGroup, proxy.Metadata.AutoRegisterGroup, proxy.Metadata.Namespace)
	if groupCfg == nil {
		adsLog.Warnf("auto registration of %v failed: cannot find WorkloadGroup %s/%s", proxy.ID, proxy.Metadata.Namespace, proxy.Metadata.AutoRegisterGroup)
		return
	}
	entry := workloadEntryFromGroup(entryName, proxy, groupCfg)
	setConnectMeta(entry, sg.Server.instanceID, con)
	_, err := sg.Store.Create(*entry)
	if err != nil {
		adsLog.Errorf("failed creating WLE for %s: %v", proxy.ID, err)
	}
}

func (sg *InternalGen) QueueUnregisterWorkload(proxy *model.Proxy) {
	// check if the WE already exists, update the status
	entryName := autoregisteredWorkloadEntryName(proxy)
	if entryName == "" {
		return
	}

	cachedEntry := sg.Store.Get(gvk.WorkloadEntry, entryName, proxy.Metadata.Namespace)
	if cachedEntry == nil || cachedEntry.Meta.Annotations[WorkloadControllerAnnotation] != sg.Server.instanceID {
		// the WLE is deleted or another contorller has taken ownership
		adsLog.Infof("skipping WLE disconnect %v", cachedEntry == nil)
		return
	}
	entry := cachedEntry.DeepCopy()

	adsLog.Infof("disconnecteing WLE %s", entryName)

	// unset controller and set disconnect timestamp, used in cleanup process
	setDisconnectMeta(&entry)
	_, err := sg.Store.Update(entry)
	if err != nil {
		adsLog.Warnf("failed to update auto-registered WorkloadEntry %s/%s: %v", proxy.Metadata.Namespace, entryName, err)
	}
}

func (sg *InternalGen) periodicWorkloadEntryCleanup(stopCh <-chan struct{}) {
	ticker := time.NewTicker(features.WorkloadEntryCleanupGracePeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			for _, ns := range sg.Store.Namespaces() {
				wles, err := sg.Store.List(gvk.WorkloadEntry, ns.Name)
				if err != nil {
					adsLog.Warnf("failed listing WorkloadEntry in %s for cleanup: %v", ns.Name, err)
					continue
				}
				for _, wle := range wles {
					// don't cleanup connected or non-autoregistered entries
					if wle.Annotations[AutoRegistrationGroupAnnotation] == "" || wle.Annotations[WorkloadControllerAnnotation] != "disconnected" {
						continue
					}
					// if we haven't passed the grace period, don't cleanup
					disconnUnixTime, err := strconv.ParseInt(wle.Annotations[DisconnectedAtAnnotation], 10, 64)
					if err != nil {
						// agressively cleanup workload entries with invalid disconnect times - they need to be re-registered and fixed.
						adsLog.Warnf("invalid disconnect time for WorkloadEntry %s/%s: %s", wle.Annotations[DisconnectedAtAnnotation])
					}
					disconnat := time.Unix(0, disconnUnixTime)
					if err != nil || time.Since(disconnat) > features.WorkloadEntryCleanupGracePeriod {
						adsLog.Infof("cleaning up %s; diconn time: %v", wle.Name, disconnat)
						if err := sg.Store.Delete(gvk.WorkloadEntry, wle.Name, wle.Namespace); err != nil {
							adsLog.Warnf("failed cleaning up auto-registered WorkloadEntry %s/%s: %v", wle.Namespace, wle.Name, err)
						}
					}
				}
			}
		case <-stopCh:
			return
		}
	}
}

func setConnectMeta(c *config.Config, controller string, con *Connection) {
	// TODO proper names, or put this in status, timestamps etc.
	c.Annotations[WorkloadControllerAnnotation] = controller
	c.Annotations[ConnectedAtAnnotation] = strconv.FormatInt(con.Connect.UnixNano(), 10)
}

func setDisconnectMeta(c *config.Config) {
	// TODO proper names, or put this in status, timestamps etc.
	c.Annotations[WorkloadControllerAnnotation] = "disconnected"
	c.Annotations[DisconnectedAtAnnotation] = strconv.FormatInt(time.Now().UnixNano(), 10)
}

func workloadEntryFromGroup(name string, proxy *model.Proxy, groupCfg *config.Config) *config.Config {
	group := groupCfg.Spec.(*v1alpha3.WorkloadGroup)
	entry := group.Template.DeepCopy()
	entry.Address = proxy.IPAddresses[0]
	if proxy.Metadata.Network != "" {
		entry.Network = proxy.Metadata.Network
	}
	if proxy.Locality != nil {
		entry.Locality = util.LocalityToString(proxy.Locality)
	}
	return &config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.WorkloadEntry,
			Name:             name,
			Namespace:        proxy.Metadata.Namespace,
			Labels:           mergeLabels(entry.Labels, proxy.Metadata.Labels),
			Annotations:      map[string]string{AutoRegistrationGroupAnnotation: groupCfg.Name},
		},
		Spec: entry,
		// TODO status fields used for garbage collection
		Status: nil,
	}
}

func mergeLabels(labels ...map[string]string) map[string]string {
	if len(labels) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(labels)*len(labels[1]))
	for _, lm := range labels {
		for k, v := range lm {
			out[k] = v
		}
	}
	return out
}

func autoregisteredWorkloadEntryName(proxy *model.Proxy) string {
	if proxy.Metadata.AutoRegisterGroup == "" {
		return ""
	}
	if len(proxy.IPAddresses) == 0 {
		adsLog.Errorf("auto registration of %v failed: missing IP addresses", proxy.ID)
		return ""
	}
	if len(proxy.Metadata.Namespace) == 0 {
		adsLog.Errorf("auto registration of %v failed: missing namespace", proxy.ID)
		return ""
	}
	p := []string{proxy.Metadata.AutoRegisterGroup, proxy.IPAddresses[0]}
	if proxy.Metadata.Network != "" {
		p = append(p, proxy.Metadata.Network)
	}
	return strings.Join(p, "-")
}

func copyConfig(c *config.Config) {

}
