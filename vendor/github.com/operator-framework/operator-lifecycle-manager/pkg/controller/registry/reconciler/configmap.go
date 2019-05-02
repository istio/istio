//go:generate counterfeiter -o ../../../fakes/fake_reconciler.go . RegistryReconciler
package reconciler

import (
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/lib/operatorclient"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/lib/operatorlister"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/lib/ownerutil"
)

var timeNow = func() metav1.Time { return metav1.NewTime(time.Now().UTC()) }

// configMapCatalogSourceDecorator wraps CatalogSource to add additional methods
type configMapCatalogSourceDecorator struct {
	*v1alpha1.CatalogSource
}

func (s *configMapCatalogSourceDecorator) serviceAccountName() string {
	return s.GetName() + "-configmap-server"
}

func (s *configMapCatalogSourceDecorator) roleName() string {
	return s.GetName() + "-configmap-reader"
}

func (s *configMapCatalogSourceDecorator) Selector() map[string]string {
	return map[string]string{
		"olm.catalogSource": s.GetName(),
	}
}

func (s *configMapCatalogSourceDecorator) Labels() map[string]string {
	labels := map[string]string{
		"olm.catalogSource": s.GetName(),
	}
	if s.Spec.SourceType == v1alpha1.SourceTypeInternal || s.Spec.SourceType == v1alpha1.SourceTypeConfigmap {
		labels["olm.configMapResourceVersion"] = s.Status.ConfigMapResource.ResourceVersion
	}
	return labels
}

func (s *configMapCatalogSourceDecorator) ConfigMapChanges(configMap *v1.ConfigMap) bool {
	if s.Status.ConfigMapResource == nil {
		return true
	}
	if s.Status.ConfigMapResource.ResourceVersion == configMap.GetResourceVersion() {
		return false
	}
	return true
}

func (s *configMapCatalogSourceDecorator) Service() *v1.Service {
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
			Selector: s.Selector(),
		},
	}
	ownerutil.AddOwner(svc, s.CatalogSource, false, false)
	return svc
}

func (s *configMapCatalogSourceDecorator) Pod(image string) *v1.Pod {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: s.GetName() + "-",
			Namespace:    s.GetNamespace(),
			Labels:       s.Labels(),
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "configmap-registry-server",
					Image: image,
					Args:  []string{"-c", s.Spec.ConfigMap, "-n", s.GetNamespace()},
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
			ServiceAccountName: s.GetName() + "-configmap-server",
		},
	}
	ownerutil.AddOwner(pod, s.CatalogSource, false, false)
	return pod
}

func (s *configMapCatalogSourceDecorator) ServiceAccount() *v1.ServiceAccount {
	sa := &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.serviceAccountName(),
			Namespace: s.GetNamespace(),
		},
	}
	ownerutil.AddOwner(sa, s.CatalogSource, false, false)
	return sa
}

func (s *configMapCatalogSourceDecorator) Role() *rbacv1.Role {
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.roleName(),
			Namespace: s.GetNamespace(),
		},
		Rules: []rbacv1.PolicyRule{
			{
				Verbs:         []string{"get"},
				APIGroups:     []string{""},
				Resources:     []string{"configmaps"},
				ResourceNames: []string{s.Spec.ConfigMap},
			},
		},
	}
	ownerutil.AddOwner(role, s.CatalogSource, false, false)
	return role
}

func (s *configMapCatalogSourceDecorator) RoleBinding() *rbacv1.RoleBinding {
	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.GetName() + "-server-configmap-reader",
			Namespace: s.GetNamespace(),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      s.serviceAccountName(),
				Namespace: s.GetNamespace(),
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     s.roleName(),
		},
	}
	ownerutil.AddOwner(rb, s.CatalogSource, false, false)
	return rb
}

type ConfigMapRegistryReconciler struct {
	Lister   operatorlister.OperatorLister
	OpClient operatorclient.ClientInterface
	Image    string
}

var _ RegistryReconciler = &ConfigMapRegistryReconciler{}

func (c *ConfigMapRegistryReconciler) currentService(source configMapCatalogSourceDecorator) *v1.Service {
	serviceName := source.Service().GetName()
	service, err := c.Lister.CoreV1().ServiceLister().Services(source.GetNamespace()).Get(serviceName)
	if err != nil {
		logrus.WithField("service", serviceName).Debug("couldn't find service in cache")
		return nil
	}
	return service
}

func (c *ConfigMapRegistryReconciler) currentServiceAccount(source configMapCatalogSourceDecorator) *v1.ServiceAccount {
	serviceAccountName := source.ServiceAccount().GetName()
	serviceAccount, err := c.Lister.CoreV1().ServiceAccountLister().ServiceAccounts(source.GetNamespace()).Get(serviceAccountName)
	if err != nil {
		logrus.WithField("serviceAccouint", serviceAccountName).WithError(err).Debug("couldn't find service account in cache")
		return nil
	}
	return serviceAccount
}

func (c *ConfigMapRegistryReconciler) currentRole(source configMapCatalogSourceDecorator) *rbacv1.Role {
	roleName := source.Role().GetName()
	role, err := c.Lister.RbacV1().RoleLister().Roles(source.GetNamespace()).Get(roleName)
	if err != nil {
		logrus.WithField("role", roleName).WithError(err).Debug("couldn't find role in cache")
		return nil
	}
	return role
}

func (c *ConfigMapRegistryReconciler) currentRoleBinding(source configMapCatalogSourceDecorator) *rbacv1.RoleBinding {
	roleBindingName := source.RoleBinding().GetName()
	roleBinding, err := c.Lister.RbacV1().RoleBindingLister().RoleBindings(source.GetNamespace()).Get(roleBindingName)
	if err != nil {
		logrus.WithField("roleBinding", roleBindingName).WithError(err).Debug("couldn't find role binding in cache")
		return nil
	}
	return roleBinding
}

func (c *ConfigMapRegistryReconciler) currentPods(source configMapCatalogSourceDecorator, image string) []*v1.Pod {
	podName := source.Pod(image).GetName()
	pods, err := c.Lister.CoreV1().PodLister().Pods(source.GetNamespace()).List(labels.SelectorFromSet(source.Selector()))
	if err != nil {
		logrus.WithField("pod", podName).WithError(err).Debug("couldn't find pod in cache")
		return nil
	}
	if len(pods) > 1 {
		logrus.WithField("selector", source.Selector()).Debug("multiple pods found for selector")
	}
	return pods
}

func (c *ConfigMapRegistryReconciler) currentPodsWithCorrectResourceVersion(source configMapCatalogSourceDecorator, image string) []*v1.Pod {
	podName := source.Pod(image).GetName()
	pods, err := c.Lister.CoreV1().PodLister().Pods(source.GetNamespace()).List(labels.SelectorFromValidatedSet(source.Labels()))
	if err != nil {
		logrus.WithField("pod", podName).WithError(err).Debug("couldn't find pod in cache")
		return nil
	}
	if len(pods) > 1 {
		logrus.WithField("selector", source.Labels()).Debug("multiple pods found for selector")
	}
	return pods
}

// Ensure that all components of registry server are up to date.
func (c *ConfigMapRegistryReconciler) EnsureRegistryServer(catalogSource *v1alpha1.CatalogSource) error {
	source := configMapCatalogSourceDecorator{catalogSource}

	image := c.Image
	if source.Spec.SourceType == "grpc" {
		image = source.Spec.Image
	}
	if image == "" {
		return fmt.Errorf("no image for registry")
	}

	// if service status is nil, we force create every object to ensure they're created the first time
	overwrite := source.Status.RegistryServiceStatus == nil
	overwritePod := overwrite

	if source.Spec.SourceType == v1alpha1.SourceTypeConfigmap || source.Spec.SourceType == v1alpha1.SourceTypeInternal {
		// fetch configmap first, exit early if we can't find it
		configMap, err := c.Lister.CoreV1().ConfigMapLister().ConfigMaps(source.GetNamespace()).Get(source.Spec.ConfigMap)
		if err != nil {
			return fmt.Errorf("unable to get configmap %s/%s from cache", source.GetNamespace(), source.Spec.ConfigMap)
		}

		if source.ConfigMapChanges(configMap) {
			catalogSource.Status.ConfigMapResource = &v1alpha1.ConfigMapResourceReference{
				Name:            configMap.GetName(),
				Namespace:       configMap.GetNamespace(),
				UID:             configMap.GetUID(),
				ResourceVersion: configMap.GetResourceVersion(),
			}

			// recreate the pod if there are configmap changes; this causes the db to be rebuilt
			overwritePod = true
		}

		// recreate the pod if no existing pod is serving the latest image
		if len(c.currentPodsWithCorrectResourceVersion(source, image)) == 0 {
			overwritePod = true
		}
	}

	//TODO: if any of these error out, we should write a status back (possibly set RegistryServiceStatus to nil so they get recreated)
	if err := c.ensureServiceAccount(source, overwrite); err != nil {
		return errors.Wrapf(err, "error ensuring service account: %s", source.serviceAccountName())
	}
	if err := c.ensureRole(source, overwrite); err != nil {
		return errors.Wrapf(err, "error ensuring role: %s", source.roleName())
	}
	if err := c.ensureRoleBinding(source, overwrite); err != nil {
		return errors.Wrapf(err, "error ensuring rolebinding: %s", source.RoleBinding().GetName())
	}
	if err := c.ensurePod(source, overwritePod); err != nil {
		return errors.Wrapf(err, "error ensuring pod: %s", source.Pod(image).GetName())
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

func (c *ConfigMapRegistryReconciler) ensureServiceAccount(source configMapCatalogSourceDecorator, overwrite bool) error {
	serviceAccount := source.ServiceAccount()
	if c.currentServiceAccount(source) != nil {
		if !overwrite {
			return nil
		}
		if err := c.OpClient.DeleteServiceAccount(serviceAccount.GetNamespace(), serviceAccount.GetName(), metav1.NewDeleteOptions(0)); err != nil {
			return err
		}
	}
	_, err := c.OpClient.CreateServiceAccount(serviceAccount)
	return err
}

func (c *ConfigMapRegistryReconciler) ensureRole(source configMapCatalogSourceDecorator, overwrite bool) error {
	role := source.Role()
	if c.currentRole(source) != nil {
		if !overwrite {
			return nil
		}
		if err := c.OpClient.DeleteRole(role.GetNamespace(), role.GetName(), metav1.NewDeleteOptions(0)); err != nil {
			return err
		}
	}
	_, err := c.OpClient.CreateRole(role)
	return err
}

func (c *ConfigMapRegistryReconciler) ensureRoleBinding(source configMapCatalogSourceDecorator, overwrite bool) error {
	roleBinding := source.RoleBinding()
	if c.currentRoleBinding(source) != nil {
		if !overwrite {
			return nil
		}
		if err := c.OpClient.DeleteRoleBinding(roleBinding.GetNamespace(), roleBinding.GetName(), metav1.NewDeleteOptions(0)); err != nil {
			return err
		}
	}
	_, err := c.OpClient.CreateRoleBinding(roleBinding)
	return err
}

func (c *ConfigMapRegistryReconciler) ensurePod(source configMapCatalogSourceDecorator, overwrite bool) error {
	pod := source.Pod(c.Image)
	currentPods := c.currentPods(source, c.Image)
	if len(currentPods) > 0 {
		if !overwrite {
			return nil
		}
		for _, p := range currentPods {
			if err := c.OpClient.KubernetesInterface().CoreV1().Pods(pod.GetNamespace()).Delete(p.GetName(), metav1.NewDeleteOptions(0)); err != nil {
				return errors.Wrapf(err, "error deleting old pod: %s", p.GetName())
			}
		}
	}
	_, err := c.OpClient.KubernetesInterface().CoreV1().Pods(pod.GetNamespace()).Create(pod)
	if err == nil {
		return nil
	}
	return errors.Wrapf(err, "error creating new pod: %s", pod.GetGenerateName())
}

func (c *ConfigMapRegistryReconciler) ensureService(source configMapCatalogSourceDecorator, overwrite bool) error {
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
