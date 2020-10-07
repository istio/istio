package xds

import (
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"strings"
	"time"
)

func (sg *InternalGen) RegisterWorkload(proxy *model.Proxy) {
	// check if the WE already exists, update the status
	entryName := autoregisteredWorkloadEntryName(proxy)
	if entryName == "" {
		adsLog.Infof("skipping WLE creation for %s", proxy.ID)
		return
	}
	entryCfg := sg.Store.Get(gvk.WorkloadEntry, entryName, proxy.Metadata.Namespace)
	if entryCfg != nil {
		setWorkloadController(entryCfg, sg.Server.instanceID)
		_, err := sg.Store.Update(*entryCfg)
		if err != nil {
			adsLog.Warn("failed to update auto-registered WorkloadEntry %s/%s: %v", proxy.Metadata.Namespace, entryName, err)
		}
		return
	}

	// create WorkloadEntry from WorkloadGroup
	groupCfg := sg.Store.Get(gvk.WorkloadGroup, proxy.Metadata.AutoRegisterGroup, proxy.Metadata.Namespace)
	if groupCfg == nil {
		adsLog.Warnf("auto registration of %v failed: cannot find WorkloadGroup %s/%s", proxy.ID, proxy.Metadata.Namespace, proxy.Metadata.AutoRegisterGroup)
		return
	}
	entry := workloadEntryFromGroup(entryName, proxy, groupCfg)
	setWorkloadController(entry, sg.Server.instanceID)
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

	entryCfg := sg.Store.Get(gvk.WorkloadEntry, entryName, proxy.Metadata.Namespace)
	if entryCfg == nil || entryCfg.Meta.Annotations["workload-controller"] != sg.Server.instanceID {
		// the WLE is deleted or another contorller has taken ownership
		return
	}

	// unset the controller
	// TODO use managed fields or PATCH
	setWorkloadController(entryCfg, "disconnected")
	_, err := sg.Store.Update(*entryCfg)
	if err != nil {
		adsLog.Warn("failed to update auto-registered WorkloadEntry %s/%s: %v", proxy.Metadata.Namespace, entryName, err)
	}
}

func (sg *InternalGen) periodicWorkloadEntryCleanup(stopCh <-chan struct{}) {
	// TODO this could fire right after a disconnect and we may not wait the full Grace period.
	// TODO we need to set disconnect timestamp
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
					if wle.Annotations["istio.io/autoRegistrationGroup"] == "" || wle.Annotations["istio.io/workloadController"] != "disconnected" {
						continue
					}
					if err := sg.Store.Delete(gvk.WorkloadEntry, wle.Name, wle.Namespace); err != nil {
						adsLog.Warnf("failed cleaning up auto-registered WorkloadEntry %s/%s: %v", wle.Namespace, wle.Name, err)
					}
				}
			}
		case <-stopCh:
			return
		}
	}
}

func setWorkloadController(c *config.Config, controller string) {
	// TODO proper names, or put this in status, timestamps etc.
	c.Annotations["istio.io/workloadController"] = controller
}

func workloadEntryFromGroup(name string, proxy *model.Proxy, groupCfg *config.Config) *config.Config {
	group := groupCfg.Spec.(*v1alpha3.WorkloadGroup)
	entry := group.Template
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
			Annotations:      map[string]string{"istio.io/autoRegistrationGroup": groupCfg.Name},
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
