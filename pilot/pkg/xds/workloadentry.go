package xds

import (
	"fmt"
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
)

func (sg *InternalGen) RegisterWorkload(proxy *model.Proxy) {
	if proxy.Metadata.AutoRegisterGroup == "" {
		return
	}
	if len(proxy.IPAddresses) == 0 {
		adsLog.Errorf("auto registration of %v failed: missing IP addresses", proxy.ID)
		return
	}
	if len(proxy.Metadata.Namespace) == 0 {
		adsLog.Errorf("auto registration of %v failed: missing namespace", proxy.ID)
		return
	}

	// check if the WE already exists, update the status
	entryName := autoregisteredWorkloadEntryName(proxy)
	entryCfg := sg.Store.Get(gvk.WorkloadEntry, entryName, proxy.Metadata.Namespace)
	if entryCfg != nil {
		// TODO put this in status/use managed fields
		entryCfg.Meta.Labels["workload-controller"] = sg.Server.instanceID
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
	group := groupCfg.Spec.(*v1alpha3.WorkloadGroup)
	entry := group.Template
	entry.Address = proxy.IPAddresses[0]
	if proxy.Metadata.Network != "" {
		entry.Network = proxy.Metadata.Network
	}
	if proxy.Locality != nil {
		entry.Locality = util.LocalityToString(proxy.Locality)
	}
	_, err := sg.Store.Create(config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.WorkloadEntry,
			Name:             entryName,
			Namespace:        proxy.Metadata.Namespace,
			Labels:           mergeLabels(entry.Labels, proxy.Metadata.Labels, map[string]string{"workload-controller": sg.Server.instanceID}),
		},
		Spec: entry,
		// TODO status fields used for garbage collection
		Status: nil,
	})
	if err != nil {
		adsLog.Errorf("failed creating WLE for %s: %v", proxy.ID, err)
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

func (sg *InternalGen) QueueUnregisterWorkload(proxy *model.Proxy) {
	// TODO cleanup with grace period
}

func autoregisteredWorkloadEntryName(proxy *model.Proxy) string {
	return fmt.Sprintf("%s-%s", proxy.Metadata.AutoRegisterGroup, proxy.IPAddresses[0])
}
