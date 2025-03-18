package ambient

import (
	"testing"

	"k8s.io/client-go/rest"

	"github.com/stretchr/testify/assert"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test"
)

func TestBuildRemoteClustersCollection(t *testing.T) {
	tests := []struct {
		name            string
		options         Options
		builder         ClientBuilder
		filter          kclient.Filter
		configOverrides []func(*rest.Config)
		expectedError   bool
	}{
		{
			name: "successful build",
			options: Options{
				Client:          kube.NewFakeClient(),
				ClusterID:       "local-cluster",
				SystemNamespace: "istio-system",
			},
			builder: func(kubeConfig []byte, clusterId cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
				return kube.NewFakeClient(), nil
			},
			filter: kclient.Filter{
				Namespace: "istio-system",
			},
			expectedError: false,
		},
		{
			name: "in-cluster config error",
			options: Options{
				Client:          kube.NewFakeClient(),
				ClusterID:       "local-cluster",
				SystemNamespace: "istio-system",
			},
			builder: func(kubeConfig []byte, clusterId cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
				return nil, assert.AnError
			},
			filter: kclient.Filter{
				Namespace: "istio-system",
			},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := krt.NewOptionsBuilder(test.NewStop(t), "test", krt.GlobalDebugHandler)
			collection := buildRemoteClustersCollection(tt.options, opts, tt.builder, tt.filter, tt.configOverrides...)

			if tt.expectedError {
				assert.Nil(t, collection)
			} else {
				assert.NotNil(t, collection)
			}
		})
	}
}
