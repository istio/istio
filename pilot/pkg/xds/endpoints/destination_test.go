// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package endpoints

import (
	"testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/destination"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test"
)

type emptyPeerAuthnPolicies struct{}
type noopUpdater struct{}

func (noopUpdater) EDSUpdate(model.ShardKey, string, string, []*model.IstioEndpoint)      {}
func (noopUpdater) EDSCacheUpdate(model.ShardKey, string, string, []*model.IstioEndpoint) {}
func (noopUpdater) SvcUpdate(model.ShardKey, string, string, model.Event)                 {}
func (noopUpdater) ConfigUpdate(*model.PushRequest)                                       {}
func (noopUpdater) ProxyUpdate(cluster.ID, string)                                        {}
func (noopUpdater) RemoveShard(model.ShardKey)                                            {}

func (emptyPeerAuthnPolicies) GetPeerAuthenticationsForWorkload(model.WorkloadPolicyMatcher) []*config.Config {
	return nil
}
func (emptyPeerAuthnPolicies) GetNamespaceMutualTLSMode(string) model.MutualTLSMode {
	return model.MTLSUnknown
}
func (emptyPeerAuthnPolicies) GetPeerAuthentications() map[string][]config.Config { return nil }
func (emptyPeerAuthnPolicies) GetGlobalMutualTLSMode() model.MutualTLSMode        { return model.MTLSUnknown }
func (emptyPeerAuthnPolicies) GetRootNamespace() string                           { return "istio-system" }
func (emptyPeerAuthnPolicies) GetVersion() string                                 { return "" }

func TestBuildDestinationLoadAssignmentWithoutService(t *testing.T) {
	resolved := destination.ResolvedDestination{
		Binding: destination.DestinationBinding{
			RuntimeName: "pool.models.internal",
			Port:        destination.DestinationPort{Name: "http", Number: 8080},
		},
		Endpoints: []*model.IstioEndpoint{{
			Addresses: []string{"10.0.0.7"}, EndpointPort: 8080,
			Namespace: "models", ServicePortName: "http",
			Labels:       map[string]string{"model": "text"},
			Locality:     model.Locality{Label: "us-central1/a/rack-1"},
			HealthStatus: model.Healthy, LbWeight: 7,
			SendUnhealthyEndpoints: true,
		}},
	}
	authn := emptyPeerAuthnPolicies{}
	proxy := &model.Proxy{
		Metadata:     &model.NodeMetadata{ClusterID: "cluster-1"},
		SidecarScope: &model.SidecarScope{AuthnPolicies: authn},
	}
	push := model.NewPushContext()
	env := model.NewEnvironment()
	configStore := model.NewFakeStore()
	env.ConfigStore = configStore
	env.Watcher = meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{})
	env.NetworksWatcher = meshwatcher.NewFixedNetworksWatcher(nil)
	env.ServiceDiscovery = &localServiceDiscovery{}
	env.AmbientIndexes = &model.NoopAmbientIndexes{}
	if err := env.InitNetworksManager(noopUpdater{}); err != nil {
		t.Fatal(err)
	}
	env.VirtualServiceController = model.NewVirtualServiceController(
		configStore, model.VSControllerOptions{KrtDebugger: krt.GlobalDebugHandler}, env.Watcher,
	)
	stop := test.NewStop(t)
	go configStore.Run(stop)
	go env.VirtualServiceController.Run(stop)
	kube.WaitForCacheSync("test", stop, configStore.HasSynced)
	kube.WaitForCacheSync("test", stop, env.VirtualServiceController.HasSynced)
	env.Init()
	push.InitContext(env, nil, nil)
	got := BuildDestinationLoadAssignment(
		proxy, push, "outbound|8080||pool.models.internal", model.TrafficDirectionOutbound, "", resolved, nil,
	)
	if got.ClusterName != "outbound|8080||pool.models.internal" || len(got.Endpoints) != 1 ||
		len(got.Endpoints[0].LbEndpoints) != 1 {
		t.Fatalf("unexpected assignment: %+v", got)
	}
	socket := got.Endpoints[0].LbEndpoints[0].GetEndpoint().GetAddress().GetSocketAddress()
	if socket.GetAddress() != "10.0.0.7" || socket.GetPortValue() != 8080 {
		t.Fatalf("unexpected endpoint address: %+v", socket)
	}
	if got.Endpoints[0].LbEndpoints[0].Metadata == nil {
		t.Fatal("destination endpoint metadata was not generated")
	}
	if locality := got.Endpoints[0].Locality; locality.Region != "us-central1" || locality.Zone != "a" || locality.SubZone != "rack-1" {
		t.Fatalf("endpoint locality lost: %+v", locality)
	}
	lb := got.Endpoints[0].LbEndpoints[0]
	if lb.HealthStatus != 1 || lb.LoadBalancingWeight.GetValue() != 7 {
		t.Fatalf("endpoint health/weight lost: %+v", lb)
	}
}
