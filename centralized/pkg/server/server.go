package server

import (
	"context"
	"fmt"
	apiv1alpha3 "istio.io/api/networking/v1alpha3"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	istioclient "istio.io/client-go/pkg/clientset/versioned"
	listerv1alpha3 "istio.io/client-go/pkg/listers/networking/v1alpha3"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/pkg/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

type Server struct {
	vsLister    listerv1alpha3.VirtualServiceLister
	gwLister    listerv1alpha3.GatewayLister
	ctx         context.Context
	queue       controllers.Queue
	client      kube.Client
	istioClient istioclient.Interface

	coreDnsBuilder *ACMGBuilder
}

func NewServer(ctx context.Context, args CoreDnsHijackArgs) (*Server, error) {
	client, err := buildKubeClient(args.KubeConfig)
	if err != nil {
		return nil, fmt.Errorf("error initializing kube client: %v", err)
	}
	s := &Server{
		client:      client,
		istioClient: client.Istio(),
		ctx:         ctx,
	}
	acmgGateway := &AcmgGateway{
		kubeClient: client.Kube(),
		gatewayData: GatewayData{
			gatewayNamespace:       args.GatewayNamespace,
			gatewayServiceName:     args.GatewayServiceName,
			istioGatewayName:       args.GateWayName,
			centralizedGateWayName: args.CentralizedGateWayName,
			gatewayDns:             args.GatewayServiceName + "." + args.GatewayNamespace + ".svc.cluster.local",
		},
	}
	coreDnsBuilder := &ACMGBuilder{
		kubeClient:  client.Kube(),
		acmgGateway: acmgGateway,
	}
	s.coreDnsBuilder = coreDnsBuilder
	s.setupHandler()
	return s, nil
}

func (s *Server) enqueueVSForAdd(obj any) {
	var name string
	var err error
	addVS := obj.(*v1alpha3.VirtualService)
	if name, err = cache.MetaNamespaceKeyFunc(addVS); err != nil {
		runtime.HandleError(err)
		return
	}
	s.queue.Add(EventItem{
		Name:          name,
		OperationType: AddVS,
		Value:         addVS,
	})
}

func (s *Server) Init() error {
	if err := s.coreDnsBuilder.InitServiceMap(); err != nil {
		return err
	}
	return nil
}

func (s *Server) enqueueVSForUpdate(new any, old any) {
	var name string
	var err error
	oldVS := old.(*v1alpha3.VirtualService)
	newVS := new.(*v1alpha3.VirtualService)
	if name, err = cache.MetaNamespaceKeyFunc(newVS); err != nil {
		runtime.HandleError(err)
		return
	}
	s.queue.Add(EventItem{
		Name:          name,
		OperationType: UpdateVS,
		Value:         newVS,
		OldValue:      oldVS,
	})
}

func (s *Server) enqueueVSForDelete(obj any) {
	var name string
	var err error
	deleteObj := obj.(*v1alpha3.VirtualService)
	name, err = cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}
	s.queue.Add(EventItem{
		Name:          name,
		OperationType: DeleteVS,
		Value:         deleteObj,
	})
}

// vsHandler Add data to the queue
func (s *Server) vsHandler() *cache.ResourceEventHandlerFuncs {
	return &cache.ResourceEventHandlerFuncs{
		AddFunc: s.enqueueVSForAdd,
		UpdateFunc: func(old, new interface{}) {
			oldVS := old.(*v1alpha3.VirtualService)
			newVS := new.(*v1alpha3.VirtualService)
			if oldVS.ResourceVersion == newVS.ResourceVersion {
				return
			}
			s.enqueueVSForUpdate(old, new)
		},
		DeleteFunc: s.enqueueVSForDelete,
	}
}

func (s *Server) getVirtualService(namespace string, name string) (*v1alpha3.VirtualService, error) {
	vs, err := s.vsLister.VirtualServices(namespace).Get(name)
	if err != nil || vs == nil {
		if err := controllers.IgnoreNotFound(err); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return vs, nil
}

func (s *Server) EnableAcmgOnVirtualService(vs *v1alpha3.VirtualService) error {
	var newVs *v1alpha3.VirtualService
	vs.DeepCopyInto(newVs)

	newVs.Spec.Gateways = append(newVs.Spec.Gateways, s.coreDnsBuilder.getIstioGatewayName())

	if _, err := s.client.Istio().NetworkingV1alpha3().VirtualServices(vs.Namespace).Update(context.TODO(), newVs, metav1.UpdateOptions{}); err != nil {
		return err
	}
	return nil
}

// Reconcile Process the data in the queue
func (s *Server) Reconcile(key any) error {
	evt := key.(EventItem)
	namespace, name, _ := cache.SplitMetaNamespaceKey(evt.Name)

	log.Infof("Reconciling VirtualService namespace %s name %s", namespace, name)
	switch evt.OperationType {
	case AddVS:
		log.Infof("[CoreDnsMap] Try to add VirtualService")
		vs, err := s.getVirtualService(namespace, name)
		if !vs.Spec.EnableAcmg {
			return nil
		}
		if err != nil || vs == nil {
			return err
		}
		if serviceOnAcmg, err := s.coreDnsBuilder.AddVSToCoreDns(vs); err != nil {
			return err
		} else {
			if err := s.coreDnsBuilder.OpenServicePortOnGateway(serviceOnAcmg); err != nil {
				return err
			}
		}
		if err = s.EnableAcmgOnVirtualService(vs); err != nil {
			return err
		}
		return nil
	case UpdateVS:
		log.Infof("[CoreDnsMap] Try to Update VirtualService")
		vs, err := s.getVirtualService(namespace, name)
		if err != nil || vs == nil {
			return err
		}
		log.Infof("[CoreDnsMap] Try to Delete Old VirtualService")
		err = s.coreDnsBuilder.deleteVSFromCoreDns(evt.OldValue)
		if err != nil {
			return err
		}
		log.Infof("[CoreDnsMap] Try to Add New VirtualService")
		if _, err := s.coreDnsBuilder.AddVSToCoreDns(vs); err != nil {
			return err
		} else {
			return nil
		}
	case DeleteVS:
		log.Infof("[CoreDnsMap] Try to delete VirtualService")
		vs, err := s.getVirtualService(namespace, name)
		if err != nil || vs != nil {
			return err
		}
		return s.coreDnsBuilder.deleteVSFromCoreDns(evt.Value)
	default:
		log.Errorf("[CoreDnsMap] UnDefined Evt Type %s", evt.OperationType)
		return nil
	}
}

func (s *Server) setupHandler() {
	log.Info("Setting up event handlers")

	s.queue = controllers.NewQueue("centralizedMesh",
		controllers.WithGenericReconciler(s.Reconcile),
		controllers.WithMaxAttempts(5),
	)
	vsInformer := s.client.IstioInformer().Networking().V1alpha3().VirtualServices()
	s.vsLister = vsInformer.Lister()

	gwInformer := s.client.IstioInformer().Networking().V1alpha3().Gateways()
	s.gwLister = gwInformer.Lister()

	vsInformer.Informer().AddEventHandler(s.vsHandler())
}

func buildKubeClient(kubeConfig string) (kube.Client, error) {
	// Used by validation
	kubeRestConfig, err := kube.DefaultRestConfig(kubeConfig, "", func(config *rest.Config) {
		config.QPS = 80
		config.Burst = 160
	})
	if err != nil {
		return nil, fmt.Errorf("failed creating kube config: %v", err)
	}

	client, err := kube.NewClient(kube.NewClientConfigForRestConfig(kubeRestConfig))
	if err != nil {
		return nil, fmt.Errorf("failed creating kube client: %v", err)
	}

	return client, nil
}

func (s *Server) Cleanup() {
	// TODO, clean up all virtual service config in coredns
}

func (s *Server) checkGateWayConfig() error {
	log.Info("check acmg gateway config...")
	gateways, err := s.gwLister.List(labels.NewSelector())
	if err != nil {
		return err
	}
	if len(gateways) == 0 {
		return s.createCentralizedGateway()
	}
	if len(gateways) > 1 {
		return fmt.Errorf("gateway num is %d, should be 1", len(gateways))
	}
	if gateways[0].Name != s.coreDnsBuilder.getIstioGatewayName() ||
		gateways[0].Namespace != s.coreDnsBuilder.getGatewayNamespace() ||
		gateways[0].Spec.Selector["app"] != s.coreDnsBuilder.getGatewayAppName() {
		return fmt.Errorf("existed gateway %s not belong to acmg", gateways[0].Name)
	}
	return nil
}

func (s *Server) checkTrafficServiceConfig() error {
	// TODO
	return nil
}

func (s *Server) getDefaultGateWay() *v1alpha3.Gateway {
	return &v1alpha3.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.coreDnsBuilder.getIstioGatewayName(),
			Namespace: s.coreDnsBuilder.getGatewayNamespace(),
		},
		Spec: apiv1alpha3.Gateway{
			Selector: map[string]string{
				"app": s.coreDnsBuilder.getGatewayAppName(),
			},
			Servers: []*apiv1alpha3.Server{
				{
					Port: &apiv1alpha3.Port{
						Number:   5000,
						Protocol: "HTTP",
						Name:     "default-server",
					},
					Hosts: []string{
						"*",
					},
				},
			},
		},
	}
}

func (s *Server) createCentralizedGateway() error {
	_, err := s.istioClient.NetworkingV1alpha3().Gateways(s.coreDnsBuilder.getGatewayNamespace()).Create(context.TODO(), s.getDefaultGateWay(), metav1.CreateOptions{})
	if err != nil {
		return err
	}
	return nil
}

func (s *Server) PreStartCheck() error {
	s.client.RunAndWait(s.ctx.Done())
	if err := s.checkGateWayConfig(); err != nil {
		return err
	}
	if err := s.checkTrafficServiceConfig(); err != nil {
		return err
	}
	return nil
}

func (s *Server) Start() {
	go func() {
		s.queue.Run(s.ctx.Done())
	}()
}
