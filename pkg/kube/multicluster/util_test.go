package multicluster

import "testing"

func TestConvertToClusterInfo(t *testing.T) {
	inputCases := []struct {
		key    string
		expect ClusterInfo
	}{
		{
			key: "123456",
			expect: ClusterInfo{
				EnableIngress: false,
				ClusterID:     "123456",
			},
		},
		{
			key: "123456__",
			expect: ClusterInfo{
				EnableIngress:       true,
				ClusterID:           "123456",
				EnableIngressStatus: true,
			},
		},
		{
			key: "123456_mse_",
			expect: ClusterInfo{
				EnableIngress:       true,
				ClusterID:           "123456",
				IngressClass:        "mse",
				EnableIngressStatus: true,
			},
		},
		{
			key: "123456_mse_default",
			expect: ClusterInfo{
				EnableIngress:       true,
				ClusterID:           "123456",
				IngressClass:        "mse",
				WatchNamespace:      "default",
				EnableIngressStatus: true,
			},
		},
		{
			key: "123456_mse_default_true",
			expect: ClusterInfo{
				EnableIngress:       true,
				ClusterID:           "123456",
				IngressClass:        "mse",
				WatchNamespace:      "default",
				EnableIngressStatus: true,
			},
		},
		{
			key: "123456_mse_default_false",
			expect: ClusterInfo{
				EnableIngress:       true,
				ClusterID:           "123456",
				IngressClass:        "mse",
				WatchNamespace:      "default",
				EnableIngressStatus: false,
			},
		},
	}

	for _, input := range inputCases {
		t.Run("", func(t *testing.T) {
			if ConvertToClusterInfo(input.key) != input.expect {
				t.Fatal("Should be equal")
			}
		})
	}
}

func TestClusterInfoEqual(t *testing.T) {
	inputCases := []struct {
		info1  ClusterInfo
		info2  ClusterInfo
		expect bool
	}{
		{
			info1: ClusterInfo{
				ClusterID: "123456",
			},
			info2: ClusterInfo{
				ClusterID: "123456",
			},
			expect: true,
		},
		{
			info1: ClusterInfo{
				EnableIngress: false,
				ClusterID:     "123456",
			},
			info2: ClusterInfo{
				EnableIngress: false,
				ClusterID:     "123456",
			},
			expect: true,
		},
		{
			info1: ClusterInfo{
				EnableIngress: false,
				ClusterID:     "123456",
			},
			info2: ClusterInfo{
				EnableIngress: true,
				ClusterID:     "123456",
			},
			expect: false,
		},
		{
			info1: ClusterInfo{
				EnableIngress: true,
				ClusterID:     "123456",
			},
			info2: ClusterInfo{
				EnableIngress: true,
				ClusterID:     "123456",
				IngressClass:  "mse",
			},
			expect: false,
		},
		{
			info1: ClusterInfo{
				EnableIngress: true,
				ClusterID:     "123456",
			},
			info2: ClusterInfo{
				EnableIngress:  true,
				ClusterID:      "123456",
				WatchNamespace: "default",
			},
			expect: false,
		},
		{
			info1: ClusterInfo{
				EnableIngress:       true,
				ClusterID:           "123456",
				IngressClass:        "mse",
				EnableIngressStatus: true,
			},
			info2: ClusterInfo{
				EnableIngress:       true,
				ClusterID:           "123456",
				IngressClass:        "mse",
				EnableIngressStatus: false,
			},
			expect: true,
		},
		{
			info1: ClusterInfo{
				EnableIngress:       true,
				ClusterID:           "123456",
				IngressClass:        "mse",
				EnableIngressStatus: true,
			},
			info2: ClusterInfo{
				EnableIngress: false,
				ClusterID:     "123456",
			},
			expect: false,
		},
	}

	for _, input := range inputCases {
		t.Run("", func(t *testing.T) {
			if input.info1.Equal(input.info2) != input.expect {
				t.Fatal("Should be equal")
			}
		})
	}
}
