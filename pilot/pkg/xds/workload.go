package xds

import (
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
)

type workloadController struct {
	store model.ConfigStore
}

func (c *workloadController) RegisterWorkload(proxy *model.Proxy) {
	if !proxy.Metadata.AutoRegister {
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

	// TODO from meta
	name := proxy.IPAddresses[0]

	wle := c.store.Get(gvk.WorkloadEntry, name, proxy.Metadata.Namespace)
	if wle != nil {
		// TODO set correct pilot and connected time
		//c.store.Update(*wle)
	}

	_, err := c.store.Create(config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.WorkloadEntry,
			Name:             name,
			Namespace:        proxy.Metadata.Namespace,
			// TODO include workloadgroup info
			Labels: proxy.Metadata.Labels,
		},
		Spec: &networking.WorkloadEntry{
			Address:        proxy.IPAddresses[0],
			Network:        proxy.Metadata.Network,
			Locality:       util.LocalityToString(proxy.Locality),
			ServiceAccount: proxy.Metadata.ServiceAccount,
			// TODO dedupe/reconcile with WLG?
			Labels: proxy.Metadata.Labels,
			// TODO from WLG
			Ports:  nil,
			Weight: 0,
		},
		// TODO status fields used for garbage collection
		Status: nil,
	})
	if err != nil {
		// TODO handle
	}
}

func (c *workloadController) UnregisterWorkload(proxy *model.Proxy) {
	// TODO set "last disconnect"
}
