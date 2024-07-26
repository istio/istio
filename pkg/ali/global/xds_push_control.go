package global

import (
	"time"

	"go.uber.org/atomic"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/monitoring"
)

var (
	inboundUpdatesMetrics = monitoring.NewSum("kubeconfig_update_inbound_updates",
		"The sum of kubconfig update inbound updates events")

	committedUpdatesMetrics = monitoring.NewSum("kubeconfig_update_committed_updates",
		"The sum of kubconfig update committed updates events")

	pushLock = log.RegisterScope("push-lock", "Push lock for protecting xds push.")

	inboundUpdates   = atomic.NewInt64(0)
	committedUpdates = atomic.NewInt64(0)

	t0 time.Time

	CacheSyncs []cache.InformerSynced
)

func init() {
	monitoring.MustRegister(inboundUpdatesMetrics)
	monitoring.MustRegister(committedUpdatesMetrics)
}

func ShouldBlockPush() bool {
	if inboundUpdates.Load() > committedUpdates.Load() {
		pushLock.Info("multi cluster secret is changing, so should block the push")
		return true
	}
	return false
}

func BlockPush() {
	if inboundUpdates.Load() <= committedUpdates.Load() {
		t0 = time.Now()
	}

	inboundUpdates.Add(1)
	inboundUpdatesMetrics.Increment()
	pushLock.Info("multi cluster secret is changing, so begin to block the push")
}

func TriggerPush(stop <-chan struct{}) {
	if inboundUpdates.Load() <= committedUpdates.Load() {
		return
	}

	success := cache.WaitForCacheSync(stop, CacheSyncs...)
	committedUpdates.Add(1)
	committedUpdatesMetrics.Increment()
	if success {
		if inboundUpdates.Load() == committedUpdates.Load() {
			pushLock.Infof("multi cluster configs have been synced, cost: %s", time.Since(t0))
		}
	} else {
		pushLock.Error("multi cluster configs synced fail.")
	}
}
