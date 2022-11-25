package server

import (
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/pkg/env"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var (
	IstioGatewayName          = env.RegisterStringVar("GATEWAY_NAME", "", "").Get()
	GatewayServiceName        = env.RegisterStringVar("GATEWAY_SERVICE_NAME", "", "").Get()
	GatewayNamespace          = env.RegisterStringVar("GATEWAY_NAMESPACE", "", "").Get()
	CentralizedGateWayAppName = env.RegisterStringVar("CENTRALIZED_GATEWAY_APP_NAME", "", "").Get()
)

type CoreDnsHijackArgs struct {
	GatewayNamespace       string
	KubeConfig             string
	GateWayName            string
	GatewayServiceName     string
	CentralizedGateWayName string
}

type OperationType uint8

const (
	UpdateVS OperationType = 0
	AddVS    OperationType = 1
	DeleteVS OperationType = 2
)

type EventItem struct {
	Name          string
	OperationType OperationType
	Value         *v1alpha3.VirtualService
	OldValue      *v1alpha3.VirtualService
}

type GatewayData struct {
	istioGatewayName       string
	centralizedGateWayName string
	gatewayServiceName     string
	gatewayNamespace       string
	gatewayDns             string
}

type AcmgGatewayPort struct {
	serviceName      string
	serviceNamespace string
	port             int32
	protocol         v1.Protocol
	targetPort       intstr.IntOrString
}

type ServiceOnAcmg struct {
	namespace string
	name      string
}
