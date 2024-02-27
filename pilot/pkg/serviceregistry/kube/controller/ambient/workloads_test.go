package ambient

import (
	securityclient "istio.io/client-go/pkg/apis/security/v1beta1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/workloadapi"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/netip"
	"testing"
)

func newAmbientUnitTest() *index {
	return &index{
		networkUpdateTrigger: krt.NewRecomputeTrigger(),
		ClusterID:            testC,
		Network: func(endpointIP string, labels labels.Instance) network.ID {
			return testNW
		},
	}
}

func TestWorkloads(t *testing.T) {
	a := newAmbientUnitTest()
	AuthorizationPolicies := krt.NewStaticCollection([]model.WorkloadAuthorization{})
	PeerAuths := krt.NewStaticCollection([]*securityclient.PeerAuthentication{})
	Waypoints := krt.NewStaticCollection([]Waypoint{})
	WorkloadServices := krt.NewStaticCollection([]model.ServiceInfo{})
	MeshConfig := krt.NewStatic(&MeshConfig{})
	builder := a.podWorkloadBuilder(MeshConfig, AuthorizationPolicies, PeerAuths, Waypoints, WorkloadServices)

	pod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "ns",
		},
		Spec: v1.PodSpec{},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
			PodIP: "1.2.3.4",
		},
	}
	res := builder(krt.TestingDummyContext{}, pod)
	assert.Equal(t, res, &model.WorkloadInfo{
		Workload: &workloadapi.Workload{
			Uid:               "cluster0//Pod/ns/name",
			Name:              "name",
			Namespace:         "ns",
			Addresses:         [][]byte{netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice()},
			Network:           testNW,
			CanonicalName:     "name",
			CanonicalRevision: "latest",
			WorkloadType:      workloadapi.WorkloadType_POD,
			WorkloadName:      "name",
			Status:            workloadapi.WorkloadStatus_UNHEALTHY,
			ClusterId:         testC,
		},
	})
}
