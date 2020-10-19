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

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
)

const (
	// TODO use status or another proper API instead of annotations

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
	if !features.WorkloadEntryAutoRegistration {
		return
	}
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
	// TODO return err from Patch through Get
	if err == nil {
		return
	} else if !errors.IsNotFound(err) && err.Error() != "item not found" {
		adsLog.Warnf("updating auto-registered WorkloadEntry %s/%s: %v", proxy.Metadata.Namespace, entryName, err)
	}

	// No WorkloadEntry, create one using fields from the associated WorkloadGroup
	groupCfg := sg.Store.Get(gvk.WorkloadGroup, proxy.Metadata.AutoRegisterGroup, proxy.Metadata.Namespace)
	if groupCfg == nil {
		adsLog.Warnf("auto-registration of %v failed: cannot find WorkloadGroup %s/%s", proxy.ID, proxy.Metadata.Namespace, proxy.Metadata.AutoRegisterGroup)
		return
	}
	entry := workloadEntryFromGroup(entryName, proxy, groupCfg)
	setConnectMeta(entry, sg.Server.instanceID, con)
	_, err = sg.Store.Create(*entry)
	if err != nil {
		// TODO retry to handle transient failures
		adsLog.Errorf("auto-registration of %v failed: error creating WorkloadEntry: %v", proxy.ID, err)
	}
}

func (sg *InternalGen) QueueUnregisterWorkload(proxy *model.Proxy) {
	if !features.WorkloadEntryAutoRegistration {
		return
	}
	// check if the WE already exists, update the status
	entryName := autoregisteredWorkloadEntryName(proxy)
	if entryName == "" {
		return
	}

	// unset controller, set disconnect time
	cfg := sg.Store.Get(gvk.WorkloadEntry, entryName, proxy.Metadata.Namespace)
	if cfg == nil {
		// we failed to create the workload entry in the first place
		return
	}
	wle := cfg.DeepCopy()
	delete(wle.Annotations, WorkloadControllerAnnotation)
	wle.Annotations[DisconnectedAtAnnotation] = strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := sg.Store.Update(wle)
	if err != nil {
		adsLog.Warnf("disconnect: failed patching WorkloadEntry %s/%s: %v", proxy.Metadata.Namespace, entryName, err)
		return
	}

	// after grace period, check if the workload ever reconnected
	ns := proxy.Metadata.Namespace
	sg.cleanupQueue.PushDelayed(func() error {
		wle := sg.Store.Get(gvk.WorkloadEntry, entryName, ns)
		if wle == nil {
			return nil
		}
		if !shouldCleanupEntry(*wle) {
			return nil
		}
		sg.cleanupEntry(*wle)
		return nil
	}, features.WorkloadEntryCleanupGracePeriod)
}

// periodicWorkloadEntryCleanup checks lists all WorkloadEntry
func (sg *InternalGen) periodicWorkloadEntryCleanup(stopCh <-chan struct{}) {
	if !features.WorkloadEntryAutoRegistration {
		return
	}
	ticker := time.NewTicker(10 * features.WorkloadEntryCleanupGracePeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			wles, err := sg.Store.List(gvk.WorkloadEntry, metav1.NamespaceAll)
			if err != nil {
				adsLog.Warnf("error listing WorkloadEntry for cleanup: %v", err)
				continue
			}
			for _, wle := range wles {
				wle := wle
				if !shouldCleanupEntry(wle) {
					continue
				}
				sg.cleanupQueue.Push(func() error {
					sg.cleanupEntry(wle)
					return nil
				})
			}
		case <-stopCh:
			return
		}
	}
}

func (sg *InternalGen) cleanupEntry(wle config.Config) {
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

	disconnUnixTime, err := strconv.Atoi(wle.Annotations[DisconnectedAtAnnotation])
	if err != nil {
		// remove workload entries with invalid disconnect times - they need to be re-registered and fixed.
		adsLog.Warnf("invalid disconnect time for WorkloadEntry %s/%s: %s", wle.Annotations[DisconnectedAtAnnotation])
	}
	disconnAt := time.Unix(0, int64(disconnUnixTime))
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
	// TODO move labels out of entry
	entry.Labels = mergeLabels(entry.Labels, proxy.Metadata.Labels)

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
			Labels:           entry.Labels,
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
		adsLog.Errorf("auto-registration of %v failed: missing IP addresses", proxy.ID)
		return ""
	}
	if len(proxy.Metadata.Namespace) == 0 {
		adsLog.Errorf("auto-registration of %v failed: missing namespace", proxy.ID)
		return ""
	}
	p := []string{proxy.Metadata.AutoRegisterGroup, proxy.IPAddresses[0]}
	if proxy.Metadata.Network != "" {
		p = append(p, proxy.Metadata.Network)
	}

	name := strings.Join(p, "-")
	if len(name) > 253 {
		name = name[len(name)-253:]
		adsLog.Warnf("generated WorkloadEntry name is too long, consider making the WorkloadGroup name shorter. Shortening from beginning to: %s", name)
	}
	return name
}
