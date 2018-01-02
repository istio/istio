package main

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"io"
	"io/ioutil"
	"istio.io/istio/pilot/platform/kube/inject"
	corev1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	rbacv1beta1 "k8s.io/api/rbac/v1beta1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apiextensionscheme "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	filepath "path"
	"strconv"
	"strings"
)

const (
	IstioNamespace       = "istio-system"
	IstioCoreYaml        = "install/kubernetes/istio.yaml"
	IstioInitializerYaml = "install/kubernetes/istio-initializer.yaml"

	// init container annotations
	AlphaAnnotation = "pod.alpha.kubernetes.io/init-containers"
	BetaAnnotation  = "pod.beta.kubernetes.io/init-containers"
	StatAnnotation  = "sidecar.istio.io/status"
	// proxy image name
	ProxyImage         = "docker.io/istio/proxy"
	InitContainerImage = "docker.io/istio/proxy_init"
)

var (
	SupportedVersions = [4]string{"0.2.6", "0.2.10", "0.2.12", "0.3.0"}
)

// Common Installer interface
type Installer interface {
	Check() (string, error)
}

type ReleaseInfo struct {
	version                string
	yamls                  map[string][]byte
	core_components        *IstioComponents
	initializer_components *IstioComponents
}

type KubeObject runtime.Object

type KubeObjectMap map[string][]runtime.Object

type KubePatch string

func (k KubePatch) Empty() bool {
	return len(k) == 0
}

func (k KubePatch) Patch() []byte {
	return []byte(k)
}

type KubeObjectsPatch struct {
	deletes map[string]KubeObject
	creates map[string]KubeObject
	updates map[string]KubePatch
}

type ComponentPatch struct {
	deployments_patch *KubeObjectsPatch
}

func (p KubeObjectsPatch) Empty() bool {
	return len(p.deletes)+len(p.creates)+len(p.updates) == 0
}

func (p *KubeObjectsPatch) Merge(s *KubeObjectsPatch) {
	for k, v := range s.deletes {
		p.deletes[k] = v.DeepCopyObject()
	}
	for k, v := range s.creates {
		p.creates[k] = v.DeepCopyObject()
	}
	for k, v := range s.updates {
		p.updates[k] = v
	}
}

type IstioComponents struct {
	istio_namespace     *corev1.Namespace
	configmaps          *corev1.ConfigMapList
	services            *corev1.ServiceList
	serviceaccounts     *corev1.ServiceAccountList
	deployments         *extensionsv1beta1.DeploymentList
	clusterroles        *rbacv1beta1.ClusterRoleList
	clusterrolebindings *rbacv1beta1.ClusterRoleBindingList
	crds                *apiextensionsv1beta1.CustomResourceDefinitionList
	//istio_config
}

func (c IstioComponents) GetDeployments() []KubeObject {
	k := make([]KubeObject, 0, len(c.deployments.Items))
	for _, d := range c.deployments.Items {
		o := d.DeepCopyObject()
		k = append(k, o)
	}
	return k
}

type KubeInstaller struct {
	client     *kubernetes.Clientset
	crd_client *apiextensionsclient.Clientset
	releases   map[string]*ReleaseInfo
}

// Check the integrity of Istio components with the corresponding release
func (i KubeInstaller) Check() (string, error) {
	c, err := i.GetInstalledComponents()
	if err != nil {
		return "", errors.Wrap(err, "Fail to get istio components")
	}
	version, err := getIstioVersion(c)
	if err != nil {
		return "", errors.Wrap(err, "Fail to get Istio version")
	}
	diff, err := i.CheckIstioComponents(c, version)
	if err != nil {
		return "", errors.Wrap(err, "Fail to check Istio components")
	}
	fmt.Printf("Diff in Istio components: %#v\n", diff)
	diff, err = i.CheckIstioSidecar(version)
	if err != nil {
		return "", errors.Wrap(err, "Fail to check Istio components")
	}
	fmt.Printf("Diff in Istio sidcar: %#v\n", diff)
	return version, nil
}

func (i KubeInstaller) CheckIstioComponents(c *IstioComponents, version string) (*KubeObjectsPatch, error) {
	d, err := i.GetReleasedComponents(version)
	if err != nil {
		return nil, errors.Wrap(err, "Fail to get Isio release for "+version)
	}
	return diffComponents(d, c)
}

func (i KubeInstaller) CheckIstioSidecar(version string) (*KubeObjectsPatch, error) {
	dl, err := i.client.ExtensionsV1beta1().Deployments(metav1.NamespaceDefault).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	patch := new(KubeObjectsPatch)
	for _, d := range dl.Items {
		p, err := checkDeployment(&d, version)
		if err != nil {
			return nil, err
		}
		if !p.Empty() {
			patch.Merge(p)
		}
	}
	return patch, nil
}

func fixAnnotation(annotation, version string) (string, error) {
	var data map[string]interface{}
	err := json.Unmarshal([]byte(annotation), &data)
	if err != nil {
		return "", err
	}
	s, ok := data["image"]
	if !ok {
		return "", errors.New("image field not found")
	}
	image, ok := s.(string)
	if !ok {
		return "", errors.New("image field is not string")
	}
	v, err := getIstioImageVersion(image)
	if err != nil {
		return "", err
	}
	if v != version {
		data["image"], err = setIstioImageVersion(image, version)
		if err != nil {
			return "", err
		}
	}
	b, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Use istio inject logic to modify the deployment
func addSidecarToDeployment(d *extensionsv1beta1.Deployment, r *ReleaseInfo) (*KubePatch, error) {
	o, ok := d.DeepCopyObject().(*extensionsv1beta1.Deployment)
	if !ok {
		return nil, errors.New("Cannot convert deployment")
	}
	params := inject.Params{
		Version: r.version,
	}
	inject.InjectIntoSpec(&params, &o.Spec.Template.Spec, &o.Spec.Template.ObjectMeta)
	original, err := d.Marshal()
	if err != nil {
		return nil, err
	}
	modified, err := o.Marshal()
	if err != nil {
		return nil, err
	}
	p, err := strategicpatch.CreateTwoWayMergePatch(original, modified, extensionsv1beta1.Deployment{})
	if err != nil {
		return nil, err
	}
	kp := KubePatch(p)
	return &kp, nil
}

func removeSidecarFromDeployment(d *extensionsv1beta1.Deployment) (*KubePatch, error) {
	o, ok := d.DeepCopyObject().(*extensionsv1beta1.Deployment)
	if !ok {
		return nil, errors.New("Cannot convert deployment")
	}
	for k, _ := range d.Spec.Template.ObjectMeta.Annotations {
		if k == AlphaAnnotation || k == BetaAnnotation || k == StatAnnotation {
			delete(o.Spec.Template.ObjectMeta.Annotations, k)
		}
	}
	for i, c := range d.Spec.Template.Spec.Containers {
		if strings.Contains(c.Image, ProxyImage) {
			cs := o.Spec.Template.Spec.Containers
			o.Spec.Template.Spec.Containers = append(cs[:i], cs[i:]...)
			break
		}
	}
	for i, c := range d.Spec.Template.Spec.InitContainers {
		if strings.Contains(c.Image, InitContainerImage) {
			cs := o.Spec.Template.Spec.InitContainers
			o.Spec.Template.Spec.InitContainers = append(cs[:i], cs[i:]...)
			break
		}
	}
	original, err := d.Marshal()
	if err != nil {
		return nil, err
	}
	modified, err := o.Marshal()
	if err != nil {
		return nil, err
	}
	p, err := strategicpatch.CreateTwoWayMergePatch(original, modified, extensionsv1beta1.Deployment{})
	if err != nil {
		return nil, err
	}
	kp := KubePatch(p)
	return &kp, nil
}

func checkDeployment(d *extensionsv1beta1.Deployment, version string) (*KubeObjectsPatch, error) {
	o, ok := d.DeepCopyObject().(*extensionsv1beta1.Deployment)
	if !ok {
		return nil, errors.New("Cannot convert deployment")
	}
	patch := new(KubeObjectsPatch)
	for k, v := range d.Spec.Template.ObjectMeta.Annotations {
		var err error
		fix := v
		switch k {
		case AlphaAnnotation:
			fix, err = fixAnnotation(v, version)
		case BetaAnnotation:
			fix, err = fixAnnotation(v, version)
		case StatAnnotation:
			// fix, err = fixStat(v, version)
		}
		if err != nil {
			return nil, err
		}
		if v != fix {
			o.Spec.Template.ObjectMeta.Annotations[k] = fix
		}
	}
	for i, c := range d.Spec.Template.Spec.Containers {
		if strings.Contains(c.Image, ProxyImage) {
			fix, err := setIstioImageVersion(c.Image, version)
			if err != nil {
				return nil, err
			}
			if c.Image != fix {
				o.Spec.Template.Spec.Containers[i].Image = fix
			}
		}
	}
	for i, c := range d.Spec.Template.Spec.InitContainers {
		if strings.Contains(c.Image, InitContainerImage) {
			fix, err := setIstioImageVersion(c.Image, version)
			if err != nil {
				return nil, err
			}
			if c.Image != fix {
				o.Spec.Template.Spec.InitContainers[i].Image = fix
			}
		}
	}
	original, err := d.Marshal()
	if err != nil {
		return nil, err
	}
	modified, err := o.Marshal()
	if err != nil {
		return nil, err
	}
	p, err := strategicpatch.CreateTwoWayMergePatch(original, modified, extensionsv1beta1.Deployment{})
	if err != nil {
		return nil, err
	}
	patch.updates[d.Name] = KubePatch(p)
	return patch, nil
}

// Return the patch that can be applied to objs_1
func diffKubeObjects(original []KubeObject, modified []KubeObject, dataStruct interface{}) (*KubeObjectsPatch, error) {
	p := KubeObjectsPatch{
		creates: map[string]KubeObject{},
		deletes: map[string]KubeObject{},
		updates: map[string]KubePatch{}}
	m1, err := genObjectMap(original)
	if err != nil {
		return nil, err
	}
	m2, err := genObjectMap(modified)
	if err != nil {
		return nil, err
	}
	for k, o := range m2 {
		_, present := m1[k]
		if !present {
			p.deletes[k] = o
		}
	}
	for k, o1 := range m1 {
		o2, present := m2[k]
		if !present {
			p.creates[k] = o1
		} else {
			s1, err := json.Marshal(o1)
			if err != nil {
				return nil, err
			}
			s2, err := json.Marshal(o2)
			if err != nil {
				return nil, err
			}
			patch, err := strategicpatch.CreateThreeWayMergePatch(s1, s1, s2, dataStruct, true)
			if err != nil {
				return nil, err
			}
			p.updates[k] = KubePatch(patch)
		}
	}
	return &p, nil
}

func genObjectMap(objs []KubeObject) (map[string]KubeObject, error) {
	m := map[string]KubeObject{}
	for _, o := range objs {
		metaobj, ok := o.(metav1.Object)
		if !ok {
			return nil, errors.New("Cannot get meta obj")
		}
		m[metaobj.GetName()] = o
	}
	return m, nil
}

// Return the patch that can be applied to modfied objects and reconcil with current objects.
func patchKubeObjects(original, modified, current []KubeObject, dataStruct interface{}) (*KubeObjectsPatch, error) {
	p := KubeObjectsPatch{
		creates: map[string]KubeObject{},
		deletes: map[string]KubeObject{},
		updates: map[string]KubePatch{}}
	m1, err := genObjectMap(original)
	if err != nil {
		return nil, err
	}
	m2, err := genObjectMap(modified)
	if err != nil {
		return nil, err
	}
	m3, err := genObjectMap(current)
	if err != nil {
		return nil, err
	}
	for k, o1 := range m1 {
		_, present := m3[k]
		if !present {
			p.deletes[k] = o1
		}
	}
	for k, o3 := range m3 {
		o2, present := m2[k]
		if !present {
			p.creates[k] = o2
		} else {
			o1, present := m1[k]
			if !present {
				// Use current object to override modified.
				o1 = o3.DeepCopyObject()
			}
			s1, err := json.Marshal(o1)
			if err != nil {
				return nil, err
			}
			s2, err := json.Marshal(o2)
			if err != nil {
				return nil, err
			}
			s3, err := json.Marshal(o3)
			if err != nil {
				return nil, err
			}
			patch, err := strategicpatch.CreateThreeWayMergePatch(s1, s2, s3, dataStruct, true)
			if err != nil {
				return nil, err
			}
			p.updates[k] = KubePatch(patch)
		}
	}
	return &p, nil
}

func diffComponents(original *IstioComponents, modified *IstioComponents) (*KubeObjectsPatch, error) {
	// Currently only compare deployment objects.
	patch, err := diffKubeObjects(original.GetDeployments(), modified.GetDeployments(), extensionsv1beta1.Deployment{})
	if err != nil {
		return nil, err
	}
	return patch, nil
}

func (i KubeInstaller) CreatePatch(version string, target string, c *IstioComponents) (*ComponentPatch, error) {
	c1, err := i.GetReleasedComponents(version)
	if err != nil {
		return nil, err
	}
	c2, err := i.GetReleasedComponents(target)
	if err != nil {
		return nil, err
	}
	// Currently only patch deployment objects
	p := new(ComponentPatch)
	p.deployments_patch, err = patchKubeObjects(c1.GetDeployments(), c.GetDeployments(), c2.GetDeployments(), extensionsv1beta1.Deployment{})
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (i KubeInstaller) ApplyPatch(p *ComponentPatch) error {
	for name, _ := range p.deployments_patch.deletes {
		err := i.client.ExtensionsV1beta1().Deployments(IstioNamespace).Delete(name, &metav1.DeleteOptions{})
		if err != nil {
			return err
		}
	}
	for _, o := range p.deployments_patch.creates {
		d, ok := o.(*extensionsv1beta1.Deployment)
		if !ok {
			return errors.New("Cannot convert deployment object")
		}
		_, err := i.client.ExtensionsV1beta1().Deployments(IstioNamespace).Create(d)
		if err != nil {
			return err
		}
	}
	for name, patch := range p.deployments_patch.updates {
		_, err := i.client.ExtensionsV1beta1().Deployments(IstioNamespace).Patch(
			name, types.StrategicMergePatchType, patch.Patch())
		if err != nil {
			return err
		}
	}
	return nil
}

// Upgrade the current version to the target version
func (i KubeInstaller) Upgrade(target string) error {
	version, err := i.Check()
	if err != nil {
		return err
	}
	if version == target {
		return nil
	} else {
		c, err := i.GetInstalledComponents()
		if err != nil {
			return err
		}

		patch, err := i.CreatePatch(version, target, c)
		if err != nil {
			return err
		}
		return i.ApplyPatch(patch)

	}
	return nil
}

func convertStringToInt(s []string) ([]int, error) {
	d := make([]int, 0, len(s))
	for _, v := range s {
		i, err := strconv.Atoi(v)
		if err != nil {
			return nil, err
		}
		d = append(d, i)
	}
	return d, nil
}

func isVersionNewer(v1, v2 string) (bool, error) {
	n1, err := convertStringToInt(strings.Split(v1, "."))
	if err != nil {
		return false, err
	}
	n2, err := convertStringToInt(strings.Split(v2, "."))
	if err != nil {
		return false, err
	}
	if len(n1) != 3 || len(n2) != 3 {
		return false, errors.New("Invalid version string.")
	}
	if n1[0] > n2[0] ||
		(n1[0] == n2[0] && n1[1] > n2[1]) ||
		(n1[0] == n2[0] && n1[1] == n2[1] && n1[2] > n2[2]) {
		return true, nil
	}
	return false, nil

}

func (i KubeInstaller) GetLatestRelease() (*ReleaseInfo, error) {
	var release *ReleaseInfo = nil
	version := "0.0.0"
	for _, r := range i.releases {
		isnewer, err := isVersionNewer(r.version, version)
		if err != nil {
			return nil, err
		}
		if isnewer {
			version = r.version
			release = r
		}
	}
	if version == "0.0.0" {
		return nil, errors.New("No available release")
	}
	return release, nil
}

// Install the target version of Istio system onto an existing cluster
func (i KubeInstaller) Install(target string) (string, error) {
	c, err := i.GetInstalledComponents()
	if err != nil {
		return "", errors.Wrap(err, "Fail to get istio components")
	}
	if len(c.GetDeployments()) != 0 {
		return "", errors.New("Istio already installed.")
	}
	r, err := i.GetLatestRelease()
	if err != nil {
		return "", errors.Wrap(err, "Fail to find latest release.")
	}
	err = i.InstallIstioComponents(r)
	if err != nil {
		return "", errors.Wrap(err, "Fail to install core components")
	}
	err = i.InstallSidecar(r)
	if err != nil {
		return "", errors.Wrap(err, "Fail to install side car")
	}
	return r.version, nil
}

func (i KubeInstaller) InstallIstioComponents(r *ReleaseInfo) error {
	for _, d := range r.core_components.deployments.Items {
		_, err := i.client.ExtensionsV1beta1().Deployments(IstioNamespace).Create(&d)
		if err != nil {
			return err
		}
	}
	for _, d := range r.initializer_components.deployments.Items {
		_, err := i.client.ExtensionsV1beta1().Deployments(IstioNamespace).Create(&d)
		if err != nil {
			return err
		}
	}
	return nil
}

func (i KubeInstaller) InstallSidecar(r *ReleaseInfo) error {
	dl, err := i.client.ExtensionsV1beta1().Deployments(metav1.NamespaceDefault).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, d := range dl.Items {
		p, err := addSidecarToDeployment(&d, r)
		if err != nil {
			return err
		}
		if !p.Empty() {
			_, err := i.client.ExtensionsV1beta1().Deployments(metav1.NamespaceDefault).Patch(
				d.Name, types.StrategicMergePatchType, p.Patch())
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// Uninstall Istio system from an existing cluster
func (i KubeInstaller) Uninstall() error {
	c, err := i.GetInstalledComponents()
	if err != nil {
		return errors.Wrap(err, "Fail to get istio components")
	}
	version, err := getIstioVersion(c)
	if err != nil {
		return errors.Wrap(err, "Fail to get Istio version")
	}
	r, err := i.GetReleaseInfo(version)
	if err != nil {
		return errors.Wrap(err, "Fail to get Isio release for "+version)
	}
	err = i.RemoveInitializer(r)
	if err != nil {
		return errors.Wrap(err, "Fail to remove initializer")
	}
	err = i.RemoveSidecar()
	if err != nil {
		return errors.Wrap(err, "Fail to remove sidecar")
	}
	err = i.RemoveIstioComponents(r)
	if err != nil {
		return errors.Wrap(err, "Fail to remove sidecar")
	}
	return nil
}

func (i KubeInstaller) RemoveInitializer(r *ReleaseInfo) error {
	for _, d := range r.initializer_components.deployments.Items {
		err := i.client.ExtensionsV1beta1().Deployments(IstioNamespace).Delete(d.Name, &metav1.DeleteOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}

func (i KubeInstaller) RemoveIstioComponents(r *ReleaseInfo) error {
	for _, d := range r.core_components.deployments.Items {
		err := i.client.ExtensionsV1beta1().Deployments(IstioNamespace).Delete(d.Name, &metav1.DeleteOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}

func (i KubeInstaller) RemoveSidecar() error {
	dl, err := i.client.ExtensionsV1beta1().Deployments(metav1.NamespaceDefault).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, d := range dl.Items {
		p, err := removeSidecarFromDeployment(&d)
		if err != nil {
			return err
		}
		if !p.Empty() {
			_, err := i.client.ExtensionsV1beta1().Deployments(metav1.NamespaceDefault).Patch(
				d.Name, types.StrategicMergePatchType, p.Patch())
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (i KubeInstaller) GetInstalledComponents() (c *IstioComponents, err error) {
	c = new(IstioComponents)
	// Globale objects
	c.clusterroles, err = i.client.RbacV1beta1().ClusterRoles().List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	c.clusterrolebindings, err = i.client.RbacV1beta1().ClusterRoleBindings().List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	// Istio namespace objects.
	c.istio_namespace, err = i.client.CoreV1().Namespaces().Get(IstioNamespace, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	c.deployments, err = i.client.ExtensionsV1beta1().Deployments(IstioNamespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	c.configmaps, err = i.client.CoreV1().ConfigMaps(IstioNamespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	c.services, err = i.client.CoreV1().Services(IstioNamespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	c.serviceaccounts, err = i.client.CoreV1().ServiceAccounts(IstioNamespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	c.crds, err = i.crd_client.ApiextensionsV1beta1().CustomResourceDefinitions().List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (i KubeInstaller) GetReleaseInfo(version string) (r *ReleaseInfo, err error) {
	r, ok := i.releases[version]
	if !ok {
		return nil, errors.New("Release info not found.")
	}
	return r, nil
}

func (i KubeInstaller) GetReleasedComponents(version string) (c *IstioComponents, err error) {
	r, err := i.GetReleaseInfo(version)
	if err != nil {
		return nil, err
	}
	return r.core_components, nil
}

func (r ReleaseInfo) DecodeYamlFiles() error {
	for f, y := range r.yamls {
		objs, err := decodeYamlFile(y)
		if err != nil {
			return err
		}
		switch f {
		case IstioCoreYaml:
			r.core_components, err = decodeIstioComponents(objs)
		case IstioInitializerYaml:
			r.initializer_components, err = decodeIstioComponents(objs)
		default:
			err = errors.New("Unknown yaml file")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func NewKubeInstallerFromLocalPath(localpath string) (i Installer, err error) {
	var ki KubeInstaller
	ki.releases, err = readReleaseInfoLocally(localpath)
	if err != nil {
		return nil, err
	}
	ki.client, ki.crd_client, err = createKubeClients(kubeconfig)
	if err != nil {
		return nil, err
	}
	return &ki, nil
}

func decodeYamlFile(inputs []byte) ([]KubeObject, error) {
	objs := make([]KubeObject, 0, 100)
	yamlDecoder := yaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(inputs)))
	for {
		buf, err := yamlDecoder.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(buf) > 0 {
			obj, _, err := scheme.Codecs.UniversalDeserializer().Decode(buf, nil, nil)
			if err != nil {
				obj, _, err = apiextensionscheme.Codecs.UniversalDeserializer().Decode(buf, nil, nil)
			}
			if err != nil {
				// TODO: decode istio objects
				continue
			}
			objs = append(objs, obj)
		}
	}
	return objs, nil
}

func decodeIstioComponents(objs []KubeObject) (*IstioComponents, error) {
	c := new(IstioComponents)
	for _, obj := range objs {
		switch obj.GetObjectKind().GroupVersionKind().Kind {
		case "Namespace":
			n, ok := obj.(*corev1.Namespace)
			if !ok {
				return nil, errors.New("Cannot convert namespace")
			}
			c.istio_namespace = new(corev1.Namespace)
			n.DeepCopyInto(c.istio_namespace)
		case "Deployment":
			d, ok := obj.(*extensionsv1beta1.Deployment)
			if !ok {
				return nil, errors.New("Cannot convert deployment")
			}
			if c.deployments == nil {
				c.deployments = new(extensionsv1beta1.DeploymentList)
			}
			c.deployments.Items = append(c.deployments.Items, extensionsv1beta1.Deployment{})
			d.DeepCopyInto(&c.deployments.Items[len(c.deployments.Items)-1])
		default:
		}
	}
	return c, nil
}

func getIstioImageVersion(image string) (string, error) {
	s := strings.Split(image, ":")
	if len(s) == 2 {
		return s[1], nil
	}
	return "", errors.New("version tag not found")
}

func setIstioImageVersion(image string, version string) (string, error) {
	s := strings.Split(image, ":")
	if len(s) == 2 {
		s[1] = version
		return strings.Join(s, ":"), nil
	}
	return "", errors.New("version tag not found")
}
func getIstioDeploymentVersion(d *extensionsv1beta1.Deployment) (string, error) {
	version := ""
	for _, container := range d.Spec.Template.Spec.Containers {
		if strings.Contains(container.Image, "docker.io/istio") {
			v, err := getIstioImageVersion(container.Image)
			if err != nil {
				return "", err
			}
			if version == "" {
				version = v
			} else if version != v {
				return "", errors.New("Inconsistent container version in " + d.Name)
			}
		}
	}
	if version == "" {
		return "", errors.New("Istio image not found")
	}
	return version, nil
}

func createClientConfig(kubeconfig string) (config *rest.Config, err error) {
	if kubeconfig == "" {
		config, err = rest.InClusterConfig()
	} else {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	return config, err
}

func createKubeClients(kubeconfig string) (*kubernetes.Clientset, *apiextensionsclient.Clientset, error) {
	restConfig, err := createClientConfig(kubeconfig)
	if err != nil {
		return nil, nil, err
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, err
	}
	ext_client, err := apiextensionsclient.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, err
	}
	return client, ext_client, nil
}

func getIstioVersion(c *IstioComponents) (string, error) {
	if len(c.deployments.Items) == 0 {
		return "", errors.New("Istio deployments are empty")
	}
	version, err := getIstioDeploymentVersion(&c.deployments.Items[0])
	if err != nil {
		return "", err
	}
	for _, d := range c.deployments.Items {
		v, err := getIstioDeploymentVersion(&d)
		if err != nil {
			return "", err
		}
		if version != v {
			return "", errors.New("Inconsistent Istio deployment versions: " + d.Name + ", " + version + " v.s. " + v)
		}
	}
	return version, nil
}

func readReleaseInfoLocally(path string) (map[string]*ReleaseInfo, error) {
	const pattern = "istio\\-*.*.*"
	var filenames = []string{
		IstioCoreYaml,
		IstioInitializerYaml,
	}
	files, err := ioutil.ReadDir(path)
	if err != nil {
		return nil, err
	}
	l := make([]string, 0, 10)
	for _, file := range files {
		if !file.IsDir() {
			continue
		}
		matched, err := filepath.Match(pattern, file.Name())
		if err != nil {
			return nil, err
		}
		if matched {
			l = append(l, file.Name())
		}
	}
	releases := map[string]*ReleaseInfo{}
	for _, dir := range l {
		r := new(ReleaseInfo)
		// Read kubernete yaml files.
		r.yamls = make(map[string][]byte)
		for _, f := range filenames {
			fp := filepath.Join(path, dir, f)
			contents, err := ioutil.ReadFile(fp)
			if err != nil {
				return nil, err
			}
			r.yamls[f] = contents
		}
		err = r.DecodeYamlFiles()
		if err != nil {
			return nil, err
		}
		r.version = dir[6:]
		releases[r.version] = r
	}
	return releases, nil
}

// Get deployment of istio components.
func runUpgradeCmd(c *cobra.Command, args []string) (err error) {
	fmt.Println("")
	return nil
}

func init() {
	upgradeCmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade Istio to a new version",
		RunE:  runUpgradeCmd,
	}
	rootCmd.AddCommand(upgradeCmd)
}
