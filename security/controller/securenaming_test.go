package controller

import (
	"reflect"
	"testing"

	"k8s.io/client-go/kubernetes/typed/core/v1/fake"
	"k8s.io/client-go/pkg/api/v1"
)

func TestGetPodServices(t *testing.T) {
	cases := []struct {
		allServices      []*v1.Service
		expectedServices []*v1.Service
		pod              *v1.Pod
	}{
		{
			allServices:      []*v1.Service{},
			expectedServices: []*v1.Service{},
			pod:              createPod(map[string]string{"app": "test-app"}),
		},
		{
			allServices:      []*v1.Service{createService("service1", nil)},
			expectedServices: []*v1.Service{},
			pod:              createPod(map[string]string{"app": "test-app"}),
		},
		{
			allServices:      []*v1.Service{createService("service1", map[string]string{"app": "prod-app"})},
			expectedServices: []*v1.Service{},
			pod:              createPod(map[string]string{"app": "test-app"}),
		},
		{
			allServices:      []*v1.Service{createService("service1", map[string]string{"app": "test-app"})},
			expectedServices: []*v1.Service{createService("service1", map[string]string{"app": "test-app"})},
			pod:              createPod(map[string]string{"app": "test-app"}),
		},
		{
			allServices:      []*v1.Service{createServiceWithNamespace("service1", "non-default", map[string]string{"app": "test-app"})},
			expectedServices: []*v1.Service{},
			pod:              createPod(map[string]string{"app": "test-app"}),
		},
		{
			allServices: []*v1.Service{
				createService("service1", map[string]string{"app": "prod-app"}),
				createService("service2", map[string]string{"app": "test-app"}),
				createService("service3", map[string]string{"version": "v1"}),
			},
			expectedServices: []*v1.Service{
				createService("service2", map[string]string{"app": "test-app"}),
				createService("service3", map[string]string{"version": "v1"}),
			},
			pod: createPod(map[string]string{"app": "test-app", "version": "v1"}),
		},
	}

	for ind, testCase := range cases {
		fakeCoreV1 := &fake.FakeCoreV1{}
		snc := NewSecureNamingController(fakeCoreV1)

		for _, service := range testCase.allServices {
			snc.serviceIndexer.Add(service)
		}

		actualServices := snc.getPodServices(testCase.pod)

		if !reflect.DeepEqual(actualServices, testCase.expectedServices) {
			t.Errorf("Case %d failed: Actual services does not match expected services\n", ind)
		}
	}
}

func createService(name string, selector map[string]string) *v1.Service {
	return createServiceWithNamespace(name, "default", selector)
}

func createServiceWithNamespace(name, namespace string, selector map[string]string) *v1.Service {
	return &v1.Service{
		ObjectMeta: v1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       v1.ServiceSpec{Selector: selector},
	}
}

func createPod(labels map[string]string) *v1.Pod {
	return &v1.Pod{ObjectMeta: v1.ObjectMeta{Labels: labels, Namespace: "default"}}
}
