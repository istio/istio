package server

import (
	"context"
	"fmt"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/pkg/log"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"strings"
)

type ACMGBuilder struct {
	kubeClient  kubernetes.Interface
	content     string
	deepNum     int
	acmgGateway *AcmgGateway
}

func (b *ACMGBuilder) getGatewayNamespace() string {
	return b.acmgGateway.gatewayData.gatewayNamespace
}

func (b *ACMGBuilder) getGatewayServiceName() string {
	return b.acmgGateway.gatewayData.gatewayServiceName
}

func (b *ACMGBuilder) getIstioGatewayName() string {
	return b.acmgGateway.gatewayData.istioGatewayName
}

func (b *ACMGBuilder) getGatewayDns() string {
	return b.acmgGateway.gatewayData.gatewayDns
}

func (b *ACMGBuilder) getGatewayAppName() string {
	return b.acmgGateway.gatewayData.centralizedGateWayName
}

func filterVirtualService(virtualService *v1alpha3.VirtualService, gatewayNamespace string, gatewayName string) bool {
	if virtualService.Namespace != gatewayNamespace {
		return false
	}
	for i := 0; i < len(virtualService.Spec.Gateways); i++ {
		if virtualService.Spec.Gateways[i] == gatewayName {
			return true
		}
	}
	return false
}

func isDnsFullName(service string) bool {
	return strings.Contains(service, ".svc.cluster.local")
}

func haveString(target string, list []string) bool {
	for _, ele := range list {
		if ele == target {
			return true
		}
	}
	return false
}

func getServiceName(dns string, namespace string) string {
	if isDnsFullName(dns) {
		index := strings.Index(dns, "."+namespace+".svc.cluster.local")
		return dns[:index]
	} else {
		return dns
	}
}

func transDnsToServiceOnAcmg(dns string) ServiceOnAcmg {
	return ServiceOnAcmg{
		namespace: getServiceNamespaceByDns(dns),
		name:      getServiceNameByDns(dns),
	}
}
func getServiceNamespaceByDns(dns string) string {
	name := strings.LastIndex(dns, ".svc.cluster.local")
	namespace := strings.LastIndex(dns[:name], ".")
	return dns[namespace+1 : name]
}

func getServiceNameByDns(dns string) string {
	name := strings.LastIndex(dns, ".svc.cluster.local")
	namespace := strings.LastIndex(dns[:name], ".")
	return dns[:namespace]
}

func cleanString(target string) (ret string) {
	ret = strings.TrimLeft(target, " ")
	ret = strings.TrimLeft(ret, "\n")
	ret = strings.TrimLeft(ret, "\t")

	ret = strings.TrimRight(ret, " ")
	ret = strings.TrimRight(ret, "\n")
	ret = strings.TrimRight(ret, "\t")
	return ret
}

func (b *ACMGBuilder) insertBlock(block []string) {
	for _, line := range block {
		b.content += "    " + line + "\n"
	}
}

func (b *ACMGBuilder) processLineForAdd(line string, block []string) {
	b.content += line + "\n"
	var haveFlag = false
	if strings.Contains(line, "{") {
		b.deepNum++
		haveFlag = true
	}
	if strings.Contains(line, "}") {
		b.deepNum--
	}
	if b.deepNum == 1 && haveFlag {
		b.insertBlock(block)
	}
	if b.deepNum < 0 {
		log.Errorf("Error deepNum is %d : %s\n", b.deepNum, line)
		return
	}
}

func (b *ACMGBuilder) processLineForDelete(line string, deleteBlock []string) {
	if !haveString(cleanString(line), deleteBlock) {
		b.content += line + "\n"
		return
	}
}

func (b *ACMGBuilder) buildNewConfigMapForAdd(oldConfigMap *v1.ConfigMap, insertBlock []string) (*v1.ConfigMap, error) {
	var newConfigMap v1.ConfigMap
	if len(insertBlock) == 0 {
		log.Warn("insertBlock is empty")
		return nil, nil
	}
	oldConfigMap.DeepCopyInto(&newConfigMap)
	for k, v := range oldConfigMap.Data {
		if k == "Corefile" {
			lines := strings.Split(v, "\n")
			for i := 0; i < len(lines); i++ {
				b.processLineForAdd(lines[i], insertBlock)
			}
		}
	}

	if b.content == "" {
		log.Errorf("Error Content is empty after build: %s", b.content)
		return &newConfigMap, fmt.Errorf("add coredns content is empty")
	}
	newConfigMap.Data["Corefile"] = b.content
	b.content = ""
	return &newConfigMap, nil
}

func (b *ACMGBuilder) buildNewConfigMapForDelete(oldConfigMap *v1.ConfigMap, deleteBlock []string) (*v1.ConfigMap, error) {
	var newConfigMap v1.ConfigMap
	if len(deleteBlock) == 0 {
		log.Warn("deleteBlock is empty")
		return nil, nil
	}
	oldConfigMap.DeepCopyInto(&newConfigMap)
	if len(deleteBlock) != 0 {
		for k, v := range oldConfigMap.Data {
			if k == "Corefile" {
				lines := strings.Split(v, "\n")
				for i := 0; i < len(lines); i++ {
					b.processLineForDelete(lines[i], deleteBlock)
				}
			}
		}
	}
	if b.content == "" {
		log.Errorf("Error content is empty after build: %s", b.content)
		return &newConfigMap, fmt.Errorf("delete coredns content is empty")
	}
	newConfigMap.Data["Corefile"] = b.content
	b.content = ""
	return &newConfigMap, nil
}

func (b *ACMGBuilder) rollBackConfigMapData(newConfigMap *v1.ConfigMap, maxRetry int) error {
	var err error
	for i := 0; i < maxRetry; i++ {
		_, err = b.kubeClient.CoreV1().ConfigMaps("kube-system").Update(context.TODO(), newConfigMap, metav1.UpdateOptions{})
		if err != nil {
			log.Errorf("rollBackConfigMapData error %s", err)
			continue
		}
		break
	}
	return err
}

func (b *ACMGBuilder) updateConfigMap(newConfigMap *v1.ConfigMap, oldConfigMap *v1.ConfigMap) error {
	_, err := b.kubeClient.CoreV1().ConfigMaps("kube-system").Update(context.TODO(), newConfigMap, metav1.UpdateOptions{})
	if err != nil {
		log.Errorf("updateConfigMap error %s", err)
		err = b.rollBackConfigMapData(oldConfigMap, 3)
		return fmt.Errorf("update coredns configMap error")
	}
	return nil
}

func (b *ACMGBuilder) AddVSToCoreDns(vs *v1alpha3.VirtualService) ([]ServiceOnAcmg, error) {
	log.Debugf("add virtualservice to coredns configmap")
	var needAddBlock []string
	serviceOnAcmgList := make([]ServiceOnAcmg, 0)
	relatedServices := make(map[string][]string)

	if filterVirtualService(vs, b.getGatewayNamespace(), b.getIstioGatewayName()) {
		for _, http := range vs.Spec.GetHttp() {
			for _, ds := range http.Route {
				relatedServices[vs.Namespace] = append(relatedServices[vs.Namespace], ds.Destination.Host)
			}
		}
	}
	for namespace, services := range relatedServices {
		var serviceFullName string
		var rewriteLine string
		for _, service := range services {
			if !isDnsFullName(service) {
				serviceFullName = service + "." + namespace + ".svc.cluster.local"
			} else {
				serviceFullName = service

			}
			serviceOnAcmg := transDnsToServiceOnAcmg(serviceFullName)
			num := Put(serviceOnAcmg)
			if num == 1 {
				rewriteLine = "rewrite name " + serviceFullName + " " + b.getGatewayDns()
				if !haveString(rewriteLine, needAddBlock) {
					needAddBlock = append(needAddBlock, rewriteLine)
				}
				serviceOnAcmgList = append(serviceOnAcmgList, serviceOnAcmg)
			}
		}
	}
	log.Infof("addBlock is : %v", needAddBlock)
	if len(needAddBlock) != 0 {
		configMap, err := b.kubeClient.CoreV1().ConfigMaps("kube-system").Get(context.TODO(), "coredns", metav1.GetOptions{})
		if err != nil {
			log.Errorf("get core dns configmap failed %s", err)
			return nil, fmt.Errorf("get core dns configmap failed")
		}
		newConfigMap, err := b.buildNewConfigMapForAdd(configMap, needAddBlock)
		if err != nil {
			log.Errorf("Error content is empty after build: %s", err)
			return nil, err
		} else {
			return serviceOnAcmgList, b.updateConfigMap(newConfigMap, configMap)
		}
	}

	return nil, nil
}

func (b *ACMGBuilder) deleteVSFromCoreDns(vs *v1alpha3.VirtualService) error {
	log.Debugf("delete virtualservice from coredns configmap")
	var needDeleteBlock []string
	relateServices := make(map[string][]string)
	if filterVirtualService(vs, b.getGatewayNamespace(), b.getIstioGatewayName()) {
		for _, http := range vs.Spec.GetHttp() {
			for _, ds := range http.Route {
				relateServices[vs.Namespace] = append(relateServices[vs.Namespace], ds.Destination.Host)
			}
		}
	}
	for k, v := range relateServices {
		var serviceFullName string
		var deleteLine string
		for _, service := range v {
			if !isDnsFullName(service) {
				serviceFullName = service + "." + k + ".svc.cluster.local"
			} else {
				serviceFullName = service
			}
			serviceOnAcmg := transDnsToServiceOnAcmg(serviceFullName)
			num := Del(serviceOnAcmg)
			if num == 0 {
				deleteLine = "rewrite name " + serviceFullName + " " + b.getGatewayDns()
				if !haveString(deleteLine, needDeleteBlock) {
					needDeleteBlock = append(needDeleteBlock, deleteLine)
				}
			}
		}
	}
	if len(needDeleteBlock) != 0 {
		configMap, err := b.kubeClient.CoreV1().ConfigMaps("kube-system").Get(context.TODO(), "coredns", metav1.GetOptions{})
		if err != nil {
			log.Errorf("get core dns configmap failed %s", err)
			return fmt.Errorf("get core dns configmap failed")
		}
		newConfigMap, err := b.buildNewConfigMapForDelete(configMap, needDeleteBlock)
		if err != nil {
			log.Errorf("Error content is empty after build: %s", err)
			return err
		} else {
			return b.updateConfigMap(newConfigMap, configMap)
		}
	}
	return nil
}

// OpenServicePortOnGateway port != targetPort port == targetPort
func (b *ACMGBuilder) OpenServicePortOnGateway(serviceOnAcmgs []ServiceOnAcmg) error {
	if len(serviceOnAcmgs) == 0 {
		return nil
	}

	acmgGatewayPorts := make([]AcmgGatewayPort, 0)
	for _, serviceOnAcmg := range serviceOnAcmgs {
		service, err := b.kubeClient.CoreV1().Services(serviceOnAcmg.namespace).Get(context.TODO(), serviceOnAcmg.name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		for _, port := range service.Spec.Ports {
			acmgGatewayPorts = append(acmgGatewayPorts, AcmgGatewayPort{
				port:             port.Port,
				targetPort:       intstr.FromInt(int(port.Port)),
				protocol:         "TCP",
				serviceName:      serviceOnAcmg.name,
				serviceNamespace: serviceOnAcmg.namespace,
			})
		}
	}
	if err := b.acmgGateway.OpenServicePortOnGatewayService(acmgGatewayPorts); err != nil {
		return err
	}
	return nil
}

func (b *ACMGBuilder) InitServiceMap() error {
	configMap, err := b.kubeClient.CoreV1().ConfigMaps("kube-system").Get(context.TODO(), "coredns", metav1.GetOptions{})
	if err != nil {
		log.Errorf("get core dns configmap failed %s", err)
		return fmt.Errorf("get core dns configmap failed")
	}
	for k, v := range configMap.Data {
		if k == "Corefile" {
			lines := strings.Split(v, "\n")
			// line ex:rewrite name helloworld.default.svc.cluster.local acmg-gateway.istio-system.svc.cluster.local
			for i := 0; i < len(lines); i++ {
				if strings.Contains(lines[i], b.getGatewayDns()) {
					Put(transDnsToServiceOnAcmg(strings.Split(lines[i], " ")[2]))
				}
			}
		}
	}
	return nil
}
