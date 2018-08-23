package controller

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"
	"k8s.io/apimachinery/pkg/util/runtime"
	listers "istio.io/istio/pkg/rpccontroller/listers/rpccontroller.istio.io/v1"
	api "k8s.io/api/core/v1"

	"istio.io/istio/pkg/api/rpccontroller.istio.io/v1"
	"istio.io/istio/pkg/rpccontroller/controller/watchers"
	"istio.io/istio/pkg/log"

	"k8s.io/client-go/kubernetes"
	"time"
	"net/http"
	"encoding/json"
	"io/ioutil"
	"strings"
)

const (
	rpcInterfaceVersion = "rpccontroller.istio.io/interface-version"
	rpcInterface				= "rpccontroller.istio.io/interface"
)

type RpcQueryResponse struct {
	Success bool 							`json:"success"`
	Data 		ServiceInterfaceData `json:"data"`
}

type ServiceInterfaceData struct {
	Providers []ServiceInterface `json:"providers"`
	Protocol  string `json:"protocal"`
}

type ServiceInterface struct {
	Interface string `json:"interface"`
	Version   string `json:"version"`
	Group   	string `json:"group"`
	Serialize string `json:"serialize"`
}

type RpcWatcher struct {
	storage      map[string]ServiceInterfaceData
	rcSyncChan   chan *v1.RpcService
	rsDeleteChan chan *v1.RpcService

	serviceUpdateChan chan *watchers.ServiceUpdate
	podUpdateChan chan *watchers.PodUpdate

	rpcServiceLister        listers.RpcServiceLister
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface

	serviceWatcher  *watchers.ServiceWatcher
	podWatcher *watchers.PodWatcher

	dnsInterface      DNSInterface

	tryLaterKeys      map[string]bool

	// map rpcservice name -> interface
	rpcInterfaceMap   map[string]string

	// map k8s service to rpcservice name
	serivice2RpcServiceMap map[string]string

	// map k8s pod to rpcservice name
	pod2RpcServiceMap map[string]string
}

func NewRpcWatcher(lister listers.RpcServiceLister, client kubernetes.Interface, config *Config, stopCh <- chan struct {}) *RpcWatcher {
	rw := &RpcWatcher{
		storage:                make(map[string]ServiceInterfaceData, 0),
		rcSyncChan:             make(chan *v1.RpcService, 10),
		rsDeleteChan:           make(chan *v1.RpcService, 10),
		serviceUpdateChan:      make(chan *watchers.ServiceUpdate, 10),
		podUpdateChan:          make(chan *watchers.PodUpdate, 10),
		rpcServiceLister:       lister,
		kubeclientset:          client,
		tryLaterKeys:           make(map[string]bool, 0),
		rpcInterfaceMap:        make(map[string]string, 0),
		serivice2RpcServiceMap: make(map[string]string, 0),
		pod2RpcServiceMap:      make(map[string]string, 0),
	}

	serviceWatcher, err := watchers.StartServiceWatcher(client, 2 * time.Second, stopCh)
	if err != nil {

	}
	rw.serviceWatcher = serviceWatcher
	serviceWatcher.RegisterHandler(rw)

	podWatcher, err := watchers.StartPodWatcher(client, 2 * time.Second, stopCh)
	if err != nil {

	}
	rw.podWatcher = podWatcher
	podWatcher.RegisterHandler(rw)

	etcdClient := NewEtcdClient(config)
	rw.dnsInterface = NewCoreDNS(etcdClient)

	return rw
}

func (rw *RpcWatcher) Run(stopCh <-chan struct{}) {
	go rw.main(stopCh)
}

func (rw *RpcWatcher) Sync(rs *v1.RpcService) {
	rw.rcSyncChan <- rs
}

func (rw *RpcWatcher) Delete(rs *v1.RpcService) {
	rw.rsDeleteChan <- rs
}

func (rw *RpcWatcher) main(stopCh <-chan struct{}) {
	t := time.NewTimer(10 * time.Second)

	for {
		select {
		case rs := <- rw.rcSyncChan:
			rw.syncRpcServiceHandler(rs)
		case rs := <- rw.rsDeleteChan:
			rw.deleteRpcServiceHandler(rs)
		case <- t.C:
			rw.timerHandler()
		case seviceUpdate := <- rw.serviceUpdateChan:
			rw.serviceHandler(seviceUpdate)
		case podUpdate := <- rw.podUpdateChan:
			rw.podHandler(podUpdate)
		case <- stopCh:
			log.Infof("rpc watcher exit")
			return
		}
	}
}

func (rw *RpcWatcher) rpcServiceName(rs *v1.RpcService) string {
	return fmt.Sprintf("%s/%s", rs.Namespace, rs.Name)
}

func (rw *RpcWatcher) serviceName(s *api.Service) string {
	return fmt.Sprintf("%s/%s", s.Namespace, s.Name)
}

func (rw *RpcWatcher) podName(s *api.Pod) string {
	return fmt.Sprintf("%s/%s", s.Namespace, s.GenerateName)
}

func (rw *RpcWatcher) deleteRpcServiceHandler(rs *v1.RpcService) {
	key := rw.rpcServiceName(rs)

	domain, exist := rw.rpcInterfaceMap[key]
	if !exist {
		log.Errorf("rpcservice %s interface not exist", key)
		return
	}

	log.Infof("delete rpcservice %s, domain: %s", key, domain)
	rw.dnsInterface.Delete(domain, rs.Spec.DomainSuffix)
}

func (rw *RpcWatcher) timerHandler() {
	if len(rw.tryLaterKeys) == 0 {
		return
	}
	keys := make([]string, len(rw.tryLaterKeys))
	for key,_ := range rw.tryLaterKeys {
		keys = append(keys, key)
	}

	rw.tryLaterKeys = make(map[string]bool, 0)

	for _, key := range keys {
		rs := rw.getRpcServiceByName(key)
		if rs == nil {
			//rw.tryLaterKeys[key] = true
			continue
		}
		rw.queryRpcInterface(key, rs)
	}
}

func (rw *RpcWatcher) queryRpcInterface(key string, rs *v1.RpcService) {
	selector := rs.Spec.Selector

	// find service by selector
	services,err := rw.serviceWatcher.ListBySelector(selector)
	if len(services) == 0 || err != nil {
		log.Errorf("service selector %v not found, err:%v", selector, err)
		return
	}
	if len(services) != 1 {
		log.Errorf("service selector %v found more than 1 service", selector)
		return
	}
	service := services[0]

	rw.serivice2RpcServiceMap[rw.serviceName(service)]= key

	if service.Spec.ClusterIP == "None" {
		log.Errorf("service %s/%s has no clusterIP", service.Namespace, service.Name)
		rw.deleteRpcServiceHandler(rs)
		return
	}

	// first add key into tryLaterKeys
	rw.tryLaterKeys[key] = true

	pods, err := rw.podWatcher.ListBySelector(service.Labels)
	if len(pods) == 0 || err != nil {
		log.Errorf("service %s/%s has no pods, err: %v, try later...", service.Namespace, service.Name, err)
		return
	}
	rw.pod2RpcServiceMap[rw.podName(pods[0])] = key

	// get pod interface
	interfaceStr := ""
	for _, pod := range pods {
		if pod.Status.Phase != "Running" {
			continue
		}

		version, exist := pod.Labels[rpcInterfaceVersion]
		if !exist {
			log.Errorf("service %s/%s pod has no %s label", service.Namespace, service.Name, rpcInterfaceVersion)
			return
		}

		i, exist := pod.Labels[rpcInterface]
		if !exist {
			log.Errorf("service %s/%s pod has no %s label", service.Namespace, service.Name, rpcInterface)
			return
		}

		log.Infof("pod %s, interface %s, version %s", pod.Name, i, version)

		url := fmt.Sprintf("http://%s:10006/rpc/interfaces", pod.Status.PodIP)
		resp, err := http.Get(url)
		if err != nil || resp.StatusCode != 200 {
			continue
		}
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Errorf("url %s resp read body error: %v", url, err)
			return
		}

		log.Infof("url: %s, resp: %s", url, string(body))

		var rpcResponse RpcQueryResponse
		err = json.Unmarshal(body, &rpcResponse)
		if err != nil {
			log.Errorf("json Unmarshal RpcResponse error: %v", err)
			return
		}

		if rpcResponse.Success != true {
			log.Errorf("resp not success")
			return
		}

		for _, inter := range(rpcResponse.Data.Providers) {
			if !strings.ContainsAny(inter.Interface, i) {
				log.Infof("interface %s, i %s", inter.Interface, i)
				continue
			}
			if inter.Version != version {
				log.Infof("version %s, i %s", inter.Version, version)
				continue
			}
			interfaceStr = inter.Interface
			break
		}

		if len(interfaceStr) == 0 {
			log.Errorf("service %s/%s interface not found", service.Namespace, service.Name)
			rw.deleteRpcServiceHandler(rs)
			return
		}

		break
	}

	// until now can delete key from tryLaterKeys map
	delete(rw.tryLaterKeys, key)

	// add <interface, clusterIP> to coreDNS
	log.Infof("update dns <%s,%s>", interfaceStr, service.Spec.ClusterIP)
	err = rw.dnsInterface.Update(interfaceStr, service.Spec.ClusterIP, rs.Spec.DomainSuffix)
	if err != nil {
		log.Errorf("update dns error: %v", err)
	}

	rw.rpcInterfaceMap[key] = interfaceStr
}

func (rw *RpcWatcher) getRpcServiceByName(key string) *v1.RpcService {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		log.Errorf("SplitMetaNamespaceKey rpcservice %s/%s error: %v", namespace, name, err)
		return nil
	}

	rs, err := rw.rpcServiceLister.RpcServices(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("rpcservice '%s' in work queue no longer exists", key))
		}
		log.Errorf("get rpcservice %s/%s error: %v", namespace, name, err)
		return nil
	}

	return rs
}

func (rw *RpcWatcher) syncRpcServiceHandler(rs *v1.RpcService) {
	key := rw.rpcServiceName(rs)
	log.Infof("rpc service:%s", key)
	rw.queryRpcInterface(key, rs)
}

func (rw *RpcWatcher) OnServiceUpdate(serviceUpdate *watchers.ServiceUpdate) {
	if serviceUpdate.Op != watchers.SYNCED {
		rw.serviceUpdateChan <- serviceUpdate
	}
}

func (rw *RpcWatcher) serviceHandler(serviceUpdate *watchers.ServiceUpdate) {
	service := serviceUpdate.Service
	op := serviceUpdate.Op

	log.Infof("service: %s/%s, op: %s/%d", service.Namespace, service.Name, watchers.OperationString[op], op)
	key, exist := rw.serivice2RpcServiceMap[rw.serviceName(service)]
	if !exist {
		log.Debugf("service %s/%s not found rpc service", service.Namespace, service.Name)
		return
	}
	rs := rw.getRpcServiceByName(key)

	if rs == nil {
		log.Debugf("rpcservice %s not found", key)
		return
	}

	switch op {
	case watchers.ADD:
		rw.syncRpcServiceHandler(rs)
	case watchers.UPDATE:
		rw.syncRpcServiceHandler(rs)
	case watchers.REMOVE:
		rw.deleteRpcServiceHandler(rs)
	}
}

func (rw *RpcWatcher)podHandler(podUpdate *watchers.PodUpdate) {
	pod := podUpdate.Pod
	op := podUpdate.Op

	log.Infof("pod: %s/%s, labels: %s, op: %s/%d", pod.Namespace, pod.Name, rw.podName(pod), watchers.OperationString[op], op)

	key, exist := rw.pod2RpcServiceMap[rw.podName(pod)]
	if !exist {
		log.Debugf("pod %s/%s not found rpc service", pod.Namespace, pod.Name)
		return
	}

	rs := rw.getRpcServiceByName(key)
	if rs == nil {
		log.Debugf("rpcservice %s not found", key)
		return
	}

	switch op {
	case watchers.ADD:
		rw.syncRpcServiceHandler(rs)
	case watchers.UPDATE:
		rw.syncRpcServiceHandler(rs)
	case watchers.REMOVE:
		rw.syncRpcServiceHandler(rs)
	}
}

func (rw *RpcWatcher)OnPodUpdate(podUpdate *watchers.PodUpdate) {
	if podUpdate.Op != watchers.SYNCED {
		rw.podUpdateChan <- podUpdate
	}
}