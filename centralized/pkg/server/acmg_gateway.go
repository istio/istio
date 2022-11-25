package server

import (
	"context"
	"fmt"
	"istio.io/pkg/log"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type AcmgGateway struct {
	kubeClient  kubernetes.Interface
	gatewayData GatewayData
}

func (a *AcmgGateway) OpenServicePortOnGatewayService(portList []AcmgGatewayPort) error {
	log.Infof("AcmgGatewayPort: %v", portList)
	gatewayService, err := a.kubeClient.CoreV1().Services(a.gatewayData.gatewayNamespace).Get(context.TODO(), a.gatewayData.gatewayServiceName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	currPortMap := make(map[int32]AcmgGatewayPort, 0)
	for _, currPort := range gatewayService.Spec.Ports {
		currPortMap[currPort.Port] = AcmgGatewayPort{
			port:       currPort.Port,
			protocol:   currPort.Protocol,
			targetPort: currPort.TargetPort,
		}
	}

	actualPortList := make([]AcmgGatewayPort, 0)

	// check if port conflict
	for _, needPort := range portList {
		if currPort, exist := currPortMap[needPort.port]; exist {
			if currPort.targetPort != needPort.targetPort {
				return fmt.Errorf("currTargetPort %v needTargetPort %v conflict on port %v", currPort.targetPort, needPort.targetPort, needPort.port)
			}
		} else {
			actualPortList = append(actualPortList, needPort)
		}
	}

	var newGatewayService v1.Service
	gatewayService.DeepCopyInto(&newGatewayService)

	for _, actualPort := range actualPortList {
		newGatewayService.Spec.Ports = append(newGatewayService.Spec.Ports, v1.ServicePort{
			Name:       actualPort.serviceNamespace + "-" + actualPort.serviceName,
			Port:       actualPort.port,
			TargetPort: actualPort.targetPort,
			Protocol:   actualPort.protocol,
		})
	}
	//var currPorts []int32
	//var needPorts []int32
	//for _, port := range gatewayService.Spec.Ports {
	//	currPorts = append(currPorts, port.Port)
	//}

	//for namespace, services := range namespaceServices {
	//	for _, serviceName := range services {
	//		service, err := b.kubeClient.CoreV1().Services(namespace).Get(context.TODO(), getServiceName(serviceName, namespace), metav1.GetOptions{})
	//		if err != nil {
	//			return err
	//		}
	//		for _, port := range service.Spec.Ports {
	//			needPorts = append(needPorts, port.Port)
	//		}
	//	}
	//}

	_, err = a.kubeClient.CoreV1().Services(a.gatewayData.gatewayNamespace).Update(context.TODO(), &newGatewayService, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}
