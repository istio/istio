package krt

import "istio.io/istio/pkg/kube"

type Syncer interface {
	WaitUntilSynced(stop <-chan struct{}) bool
}

var (
	_ Syncer = channelSyncer{}
	_ Syncer = pollSyncer{}
)

type channelSyncer struct {
	name   string
	synced <-chan struct{}
}

func (c channelSyncer) WaitUntilSynced(stop <-chan struct{}) bool {
	return waitForCacheSync(c.name, stop, c.synced)
}

type pollSyncer struct {
	name string
	f    func() bool
}

func (c pollSyncer) WaitUntilSynced(stop <-chan struct{}) bool {
	return kube.WaitForCacheSync(c.name, stop, c.f)
}

type alwaysSynced struct{}

func (c alwaysSynced) WaitUntilSynced(stop <-chan struct{}) bool {
	return true
}
