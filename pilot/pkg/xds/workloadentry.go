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
	"context"
	"strconv"
	"strings"
	"time"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
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

	// Try to patch, if it fails then try to create
	_, err := sg.Store.Patch(gvk.WorkloadEntry, entryName, proxy.Metadata.Namespace, func(cfg config.Config) config.Config {
		setConnectMeta(&cfg, sg.Server.instanceID, con)
		return cfg
	})
	if err == nil {
		return
	}
	// TODO better errors from Get/Patch. NotFound is fine but others should warn.

	// No WorkloadEntry, create one using fields from the associated WorkloadGroup
	groupCfg := sg.Store.Get(gvk.WorkloadGroup, proxy.Metadata.AutoRegisterGroup, proxy.Metadata.Namespace)
	if groupCfg == nil {
		adsLog.Warnf("auto registration of %v failed: cannot find WorkloadGroup %s/%s", proxy.ID, proxy.Metadata.Namespace, proxy.Metadata.AutoRegisterGroup)
		return
	}
	entry := workloadEntryFromGroup(entryName, proxy, groupCfg)
	setConnectMeta(entry, sg.Server.instanceID, con)
	_, err = sg.Store.Create(*entry)
	if err != nil {
		adsLog.Errorf("auto registration of %v failed: error creating WorkloadEntry: %v", proxy.ID, err)
	}
}

func (sg *InternalGen) QueueUnregisterWorkload(proxy *model.Proxy) {
	// check if the WE already exists, update the status
	entryName := autoregisteredWorkloadEntryName(proxy)
	if entryName == "" {
		return
	}
	// unset controller, set disconnect time
	_, err := sg.Store.Patch(gvk.WorkloadEntry, entryName, proxy.Metadata.Namespace, func(cfg config.Config) config.Config {
		delete(cfg.Annotations, WorkloadControllerAnnotation)
		cfg.Annotations[DisconnectedAtAnnotation] = strconv.FormatInt(time.Now().UnixNano(), 10)
		return cfg
	})
	if err != nil {
		adsLog.Warnf("disconnect: failed patching WorkloadEntry %s/%s: %v", proxy.Metadata.Namespace, entryName, err)
		return
	}

	gp := time.After(features.WorkloadEntryCleanupGracePeriod)
	go func() {
		<-gp
		sg.delayedCleanup <- config.Meta{Name: entryName, Namespace: proxy.Metadata.Namespace}
	}()

}

// workloadEntryCleanup processes the delete queue as well as performs periodic cleanup of WorkloadEntries to catch
// edge cases where the last connected pilot cannot perform the cleanup.
func (sg *InternalGen) workloadEntryCleanup(stopCh <-chan struct{}) {
	ticker := time.NewTicker(10 * features.WorkloadEntryCleanupGracePeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			for _, ns := range sg.Store.Namespaces() {
				wles, err := sg.Store.List(gvk.WorkloadEntry, ns.Name)
				if err != nil {
					continue
				}
				for _, wle := range wles {
					go sg.cleanupEntry(wle)
				}
			}
		case weMeta := <-sg.delayedCleanup:
			wle := sg.Store.Get(gvk.WorkloadEntry, weMeta.Name, weMeta.Namespace)
			if wle == nil {
				continue
			}
			go sg.cleanupEntry(*wle)
		case <-stopCh:
			return
		}
	}
}

func (sg *InternalGen) cleanupEntry(wle config.Config) {
	if !shouldCleanupEntry(wle) {
		return
	}
	if err := sg.cleanupLimit.Wait(context.TODO()); err != nil {
		adsLog.Errorf("error in WorkloadEntry cleanup rate limiter: %v", err)
	}
	if err := sg.Store.Delete(gvk.WorkloadEntry, wle.Name, wle.Namespace); err != nil {
		adsLog.Warnf("failed cleaning up auto-registered WorkloadEntry %s/%s: %v", wle.Namespace, wle.Name, err)
	}
}

func shouldCleanupEntry(wle config.Config) bool {
	// don't clean-up if connected or non-autoregistered WorkloadEntries
	_, ok := wle.Annotations[WorkloadControllerAnnotation]
	if wle.Annotations[AutoRegistrationGroupAnnotation] == "" || ok {
		return false
	}

	disconnUnixTime, err := strconv.ParseInt(wle.Annotations[DisconnectedAtAnnotation], 10, 64)
	if err != nil {
		// remove workload entries with invalid disconnect times - they need to be re-registered and fixed.
		adsLog.Warnf("invalid disconnect time for WorkloadEntry %s/%s: %s", wle.Annotations[DisconnectedAtAnnotation])
	}
	disconnAt := time.Unix(0, disconnUnixTime)
	// if we haven't passed the grace period, don't cleanup
	if err == nil && time.Since(disconnAt) < features.WorkloadEntryCleanupGracePeriod {
		return false
	}

	return true
}

func setConnectMeta(c *config.Config, controller string, con *Connection) {
	c.Annotations[WorkloadControllerAnnotation] = controller
	c.Annotations[ConnectedAtAnnotation] = strconv.FormatInt(con.Connect.UnixNano(), 10)
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
