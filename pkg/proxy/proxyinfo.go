package proxy

import (
	"context"
	"encoding/json"
	"fmt"

	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/kube"
	istioVersion "istio.io/pkg/version"
)

type sidecarSyncStatus struct {
	// nolint: structcheck, unused
	pilot string
	xds.SyncStatus
}

func GetProxyInfo(kubeconfig, configContext, revision, istioNamespace string) (*[]istioVersion.ProxyInfo, error) {
	config, err := kube.DefaultRestConfig(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}
	kubeClient, err := kube.NewClientWithRevision(config, revision)
	if err != nil {
		return nil, err
	}
	// Ask Pilot for the Envoy sidecar sync status, which includes the sidecar version info
	allSyncz, err := kubeClient.AllDiscoveryDo(context.TODO(), istioNamespace, "/debug/syncz")
	if err != nil {
		return nil, err
	}

	pi := []istioVersion.ProxyInfo{}
	for _, syncz := range allSyncz {
		var sss []*sidecarSyncStatus
		err = json.Unmarshal(syncz, &sss)
		if err != nil {
			return nil, err
		}

		for _, ss := range sss {
			pi = append(pi, istioVersion.ProxyInfo{
				ID:           ss.ProxyID,
				IstioVersion: ss.SyncStatus.IstioVersion,
			})
		}
	}

	return &pi, nil
}

// GetIDsFromProxyInfo is a helper function to retrieve list of IDs from Proxy.
func GetIDsFromProxyInfo(kubeconfig, configContext, revision, istioNamespace string) ([]string, error) {
	var IDs []string
	pi, err := GetProxyInfo(kubeconfig, configContext, revision, istioNamespace)
	if err != nil {
		return IDs, fmt.Errorf("failed to get proxy infos: %v", err)
	}
	for _, pi := range *pi {
		IDs = append(IDs, pi.ID)
	}
	return IDs, nil
}
