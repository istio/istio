package reconciler

import (
	"fmt"

	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/lib/operatorclient"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/lib/operatorlister"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/lib/ownerutil"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// grpcCatalogSourceDecorator wraps CatalogSource to add additional methods
type grpcCatalogSourceDecorator struct {
	*v1alpha1.CatalogSource
}

func (s *grpcCatalogSourceDecorator) Selector() labels.Selector {
	return labels.SelectorFromValidatedSet(map[string]string{
		"olm.catalogSource": s.GetName(),
	})
}

func (s *grpcCatalogSourceDecorator) Labels() map[string]string {
	return map[string]string{
		"olm.catalogSource": s.GetName(),
	}
}

func (s *grpcCatalogSourceDecorator) Service() *v1.Service {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.GetName(),
			Namespace: s.GetNamespace(),
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Name:       "grpc",
					Port:       50051,
					TargetPort: intstr.FromInt(50051),
				},
			},
			Selector: s.Labels(),
		},
	}
	ownerutil.AddOwner(svc, s.CatalogSource, false, false)
	return svc
}

func (s *grpcCatalogSourceDecorator) Pod() *v1.Pod {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: s.GetName() + "-",
			Namespace:    s.GetNamespace(),
			Labels:       s.Labels(),
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "registry-server",
					Image: s.Spec.Image,
					Ports: []v1.ContainerPort{
						{
							Name:          "grpc",
							ContainerPort: 50051,
						},
					},
					ReadinessProbe: &v1.Probe{
						Handler: v1.Handler{
							Exec: &v1.ExecAction{
								Command: []string{"grpc_health_probe", "-addr=localhost:50051"},
							},
						},
						InitialDelaySeconds: 5,
					},
					LivenessProbe: &v1.Probe{
						Handler: v1.Handler{
							Exec: &v1.ExecAction{
								Command: []string{"grpc_health_probe", "-addr=localhost:50051"},
							},
						},
						InitialDelaySeconds: 10,
					},
				},
			},
		},
	}
	ownerutil.AddOwner(pod, s.CatalogSource, false, false)
	return pod
}

type GrpcRegistryReconciler struct {
	Lister   operatorlister.OperatorLister
	OpClient operatorclient.ClientInterface
}

var _ RegistryReconciler = &GrpcRegistryReconciler{}

func (c *GrpcRegistryReconciler) currentService(source grpcCatalogSourceDecorator) *v1.Service {
	serviceName := source.Service().GetName()
	service, err := c.Lister.CoreV1().ServiceLister().Services(source.GetNamespace()).Get(serviceName)
	if err != nil {
		logrus.WithField("service", serviceName).Warn("couldn't find service in cache")
		return nil
	}
	return service
}

func (c *GrpcRegistryReconciler) currentPods(source grpcCatalogSourceDecorator) []*v1.Pod {
	pods, err := c.Lister.CoreV1().PodLister().Pods(source.GetNamespace()).List(source.Selector())
	if err != nil {
		logrus.WithError(err).Warn("couldn't find pod in cache")
		return nil
	}
	if len(pods) > 1 {
		logrus.WithField("selector", source.Selector()).Warn("multiple pods found for selector")
	}
	return pods
}

func (c *GrpcRegistryReconciler) currentPodsWithCorrectImage(source grpcCatalogSourceDecorator) []*v1.Pod {
	pods, err := c.Lister.CoreV1().PodLister().Pods(source.GetNamespace()).List(labels.SelectorFromValidatedSet(source.Labels()))
	if err != nil {
		logrus.WithError(err).Warn("couldn't find pod in cache")
		return nil
	}
	found := []*v1.Pod{}
	for _, p := range pods {
		if p.Spec.Containers[0].Image == source.Spec.Image {
			found = append(found, p)
		}
	}
	return found
}

// Ensure that all components of registry server are up to date.
func (c *GrpcRegistryReconciler) EnsureRegistryServer(catalogSource *v1alpha1.CatalogSource) error {
	source := grpcCatalogSourceDecorator{catalogSource}

	// if service status is nil, we force create every object to ensure they're created the first time
	overwrite := source.Status.RegistryServiceStatus == nil
	// recreate the pod if no existing pod is serving the latest image
	overwritePod := overwrite || len(c.currentPodsWithCorrectImage(source)) == 0

	//TODO: if any of these error out, we should write a status back (possibly set RegistryServiceStatus to nil so they get recreated)
	if err := c.ensurePod(source, overwritePod); err != nil {
		return errors.Wrapf(err, "error ensuring pod: %s", source.Pod().GetName())
	}
	if err := c.ensureService(source, overwrite); err != nil {
		return errors.Wrapf(err, "error ensuring service: %s", source.Service().GetName())
	}

	if overwritePod {
		catalogSource.Status.RegistryServiceStatus = &v1alpha1.RegistryServiceStatus{
			CreatedAt:        timeNow(),
			Protocol:         "grpc",
			ServiceName:      source.Service().GetName(),
			ServiceNamespace: source.GetNamespace(),
			Port:             fmt.Sprintf("%d", source.Service().Spec.Ports[0].Port),
		}
		catalogSource.Status.LastSync = timeNow()
	}
	return nil
}

func (c *GrpcRegistryReconciler) ensurePod(source grpcCatalogSourceDecorator, overwrite bool) error {
	currentPods := c.currentPods(source)
	if len(currentPods) > 0 {
		if !overwrite {
			return nil
		}
		for _, p := range currentPods {
			if err := c.OpClient.KubernetesInterface().CoreV1().Pods(source.GetNamespace()).Delete(p.GetName(), metav1.NewDeleteOptions(0)); err != nil {
				return errors.Wrapf(err, "error deleting old pod: %s", p.GetName())
			}
		}
	}
	_, err := c.OpClient.KubernetesInterface().CoreV1().Pods(source.GetNamespace()).Create(source.Pod())
	if err == nil {
		return nil
	}
	return errors.Wrapf(err, "error creating new pod: %s", source.Pod().GetGenerateName())
}

func (c *GrpcRegistryReconciler) ensureService(source grpcCatalogSourceDecorator, overwrite bool) error {
	service := source.Service()
	if c.currentService(source) != nil {
		if !overwrite {
			return nil
		}
		if err := c.OpClient.DeleteService(service.GetNamespace(), service.GetName(), metav1.NewDeleteOptions(0)); err != nil {
			return err
		}
	}
	_, err := c.OpClient.CreateService(service)
	return err
}
