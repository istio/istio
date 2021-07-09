package translate

import (
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var (
	httpsPort = &v1.ServicePort{
		Name:       "https",
		Protocol:   v1.ProtocolTCP,
		Port:       443,
		TargetPort: intstr.IntOrString{IntVal: 8443},
	}
	quicPort = &v1.ServicePort{
		Name:       "http3-quic",
		Protocol:   v1.ProtocolUDP,
		Port:       443,
		TargetPort: intstr.IntOrString{IntVal: 8443},
	}
	httpPort = &v1.ServicePort{
		Name:       "http-port",
		Protocol:   v1.ProtocolTCP,
		Port:       80,
		TargetPort: intstr.IntOrString{IntVal: 8080},
	}
	mysqlPort = &v1.ServicePort{
		Name:     "mysql-port",
		Protocol: v1.ProtocolTCP,
		Port:     3306,
	}
)

func TestStrategicPortMergeByPortAndProtocol(t *testing.T) {
	for _, tt := range []struct {
		name                string
		basePorts           []*v1.ServicePort
		overlayPorts        []*v1.ServicePort
		expectedMergedPorts []*v1.ServicePort
	}{
		{
			name:                "both base and overlay are nil",
			basePorts:           nil,
			overlayPorts:        nil,
			expectedMergedPorts: nil,
		},
		{
			name:                "overlay is nil",
			basePorts:           []*v1.ServicePort{httpPort, httpsPort, quicPort},
			overlayPorts:        nil,
			expectedMergedPorts: []*v1.ServicePort{httpPort, httpsPort, quicPort},
		},
		{
			name:                "base is nil",
			basePorts:           nil,
			overlayPorts:        []*v1.ServicePort{httpPort, httpsPort, quicPort},
			expectedMergedPorts: []*v1.ServicePort{httpPort, httpsPort, quicPort},
		},
		{
			name:                "same base and overlay",
			basePorts:           []*v1.ServicePort{httpPort, httpsPort},
			overlayPorts:        []*v1.ServicePort{httpsPort, httpPort},
			expectedMergedPorts: []*v1.ServicePort{httpPort, httpsPort},
		},
		{
			name:                "base and overlay for the same port, different protocol",
			basePorts:           []*v1.ServicePort{httpPort, httpsPort, mysqlPort},
			overlayPorts:        []*v1.ServicePort{quicPort},
			expectedMergedPorts: []*v1.ServicePort{httpPort, quicPort, httpsPort, mysqlPort},
		},
		{
			name:                "base and overlay with different ports",
			basePorts:           []*v1.ServicePort{httpPort},
			overlayPorts:        []*v1.ServicePort{httpsPort},
			expectedMergedPorts: []*v1.ServicePort{httpPort, httpsPort},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			actualMergedPorts := strategicMergePorts(tt.basePorts, tt.overlayPorts)
			portCompareFn := func(ps []*v1.ServicePort) func(int, int) bool {
				return func(i, j int) bool {
					if ps[i].Port != ps[j].Port {
						return ps[i].Port < ps[j].Port
					}
					return ps[i].Protocol < ps[j].Protocol
				}
			}
			sort.Slice(tt.expectedMergedPorts, portCompareFn(tt.expectedMergedPorts))
			sort.Slice(actualMergedPorts, portCompareFn(actualMergedPorts))
			if diff := cmp.Diff(actualMergedPorts, tt.expectedMergedPorts); diff != "" {
				t.Fatalf("expected differs from actual. Diff:\n%s", diff)
			}
		})
	}
}
