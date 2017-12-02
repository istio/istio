package main

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"io"
	"io/ioutil"
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
	"strings"
)

const (
	IstioNamespace         = "istio-system"
	InitializerDeployment  = "istio-initializer"
	InitializerContainer   = "initializer"
	InitializerImagePrefix = "docker.io/istio/sidecar_initializer"
)

var (
	SupportedVersions = [4]string{"0.2.6", "0.2.10", "0.2.12", "0.3.0"}
)

// Common Installer interface
type Installer interface {
	Check() (string, error)
}

type ReleaseInfo struct {
	version string
	yamls   map[string][]byte
}

type KubeObject runtime.Object

type KubeObjectMap map[string][]runtime.Object

type KubePatch string

type KubeObjectsPatch struct {
	deletes map[string]KubeObject
	creates map[string]KubeObject
	updates map[string]KubePatch
}

type ComponentPatch struct {
	deployments_patch KubeObjectsPatch
}

func (p KubeObjectsPatch) Empty() bool {
	return len(p.deletes)+len(p.creates)+len(p.updates) == 0
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
	k := make([]KubeObject, 0, 10)
	for _, d := range c.deployments.Items {
		o := d.DeepCopyObject()
		k = append(k, o)
	}
	return k
}

type KubeInstaller struct {
	client     *kubernetes.Clientset
	crd_client *apiextensionsclient.Clientset
	releases   []ReleaseInfo
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
	d, err := i.GetReleasedComponents(version)
	if err != nil {
		return "", errors.Wrap(err, "Fail to get Isio release for "+version)
	}
	p, err := diffComponents(c, d)
	if err != nil {
		return "", errors.Wrap(err, "Fail to calculate the diff.")
	}
	if p.Empty() {
		return version, nil
	}
	return "", errors.Errorf("Diff: %+v", *p)
}

// Return the patch that can be applied to objs_1
func diffKubeObjects(objs_1 []KubeObject, objs_2 []KubeObject, dataStruct interface{}) (*KubeObjectsPatch, error) {
	p := KubeObjectsPatch{
		creates: make(map[string]KubeObject),
		deletes: make(map[string]KubeObject),
		updates: make(map[string]KubePatch),
	}
	m1 := make(map[string]KubeObject)
	m2 := make(map[string]KubeObject)
	for _, o := range objs_1 {
		metaobj, ok := o.(metav1.Object)
		if !ok {
			return nil, errors.New("Cannot get meta obj")
		}
		m1[metaobj.GetName()] = o
	}
	for _, o := range objs_2 {
		metaobj, ok := o.(metav1.Object)
		if !ok {
			return nil, errors.New("Cannot get meta obj")
		}
		m2[metaobj.GetName()] = o
	}
	for k, o1 := range m2 {
		o2, present := m1[k]
		if !present {
			p.creates[k] = o1
		} else {
			s1, err := json.Marshal(o1)
			s2, err := json.Marshal(o2)
			patch, err := strategicpatch.CreateTwoWayMergePatch(s1, s2, dataStruct)
			if err != nil {
				return nil, err
			}
			p.updates[k] = KubePatch(patch)
		}
	}
	for k, o1 := range m1 {
		_, present := m2[k]
		if !present {
			p.deletes[k] = o1
		}
	}
	return &p, nil
}

func diffComponents(c1 *IstioComponents, c2 *IstioComponents) (*KubeObjectsPatch, error) {
	patch, err := diffKubeObjects(c1.GetDeployments(), c2.GetDeployments(), extensionsv1beta1.Deployment{})
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
	// Creating three-way patch
	diff1, err := diffKubeObjects(c1.GetDeployments(), c2.GetDeployments(), extensionsv1beta1.Deployment{})
	if err != nil {
		return nil, err
	}
	diff2, err := diffKubeObjects(c.GetDeployments(), c2.GetDeployments(), extensionsv1beta1.Deployment{})
	if err != nil {
		return nil, err
	}
	p := new(ComponentPatch)
	p.deployments_patch.deletes = diff1.deletes
	p.deployments_patch.creates = diff2.creates
	p.deployments_patch.updates = diff2.updates
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
		_, err := i.client.ExtensionsV1beta1().Deployments(IstioNamespace).Patch(name, types.StrategicMergePatchType, []byte(patch))
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

// Install the target version of Istio system onto an existing cluster
func (i KubeInstaller) Install(target string) (string, error) {
	return "", errors.New("Unimplemented")
}

// Uninstall Istio system from an existing cluster
func (i KubeInstaller) Uninstall() (string, error) {
	return "", errors.New("Unimplemented")
}

func (i KubeInstaller) Fix() (string, error) {
	return "", errors.New("Unimplemented")
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

func (i KubeInstaller) GetReleasedComponents(version string) (c *IstioComponents, err error) {
	for _, r := range i.releases {
		if r.version == version {
			for f, y := range r.yamls {
				fmt.Println("Decoding " + f)
				objs, err := decodeYamlFile(y)
				if err != nil {
					return nil, err
				}
				return decodeIstioComponents(objs)
			}
		}
	}
	return nil, errors.New("release data not found")
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
				// TODO: decode istio crd object
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
				return nil, errors.New("Cannot convert namespace")
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

func getIstioImageVersion(d *extensionsv1beta1.Deployment) (string, error) {
	version := ""
	for _, container := range d.Spec.Template.Spec.Containers {
		if strings.Contains(container.Image, "docker.io/istio") {
			s := strings.Split(container.Image, ":")
			if len(s) == 2 {
				if version == "" {
					version = s[1]
				} else if version != s[1] {
					return "", errors.New("Inconsistent container version in " + d.Name)
				}
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
	version, err := getIstioImageVersion(&c.deployments.Items[0])
	if err != nil {
		return "", err
	}
	for _, d := range c.deployments.Items {
		v, err := getIstioImageVersion(&d)
		if err != nil {
			return "", err
		}
		if version != v {
			return "", errors.New("Inconsistent Istio deployment versions: " + d.Name + ", " + version + " v.s. " + v)
		}
	}
	return version, nil
}

func readReleaseInfoLocally(path string) ([]ReleaseInfo, error) {
	const pattern = "istio\\-*.*.*"
	const filename = "install/kubernetes/istio.yaml"
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
	releases := make([]ReleaseInfo, len(l))
	for i, dir := range l {
		// Read kubernete yaml files.
		releases[i].version = dir[6:]
		releases[i].yamls = make(map[string][]byte)
		f := filepath.Join(path, dir, filename)
		contents, err := ioutil.ReadFile(f)
		if err != nil {
			return nil, err
		}
		releases[i].yamls[filename] = contents
	}
	return releases, nil
}

// Get deployment of istio components.
func runUpgradeCmd(c *cobra.Command, args []string) (err error) {
	fmt.Println("")
	return nil
}

func runTestCmd(c *cobra.Command, args []string) (err error) {
	f := "/usr/local/google/home/yusuo/istio-0.2.10/install/kubernetes/istio.yaml"
	contents, err := ioutil.ReadFile(f)
	if err != nil {
		return nil
	}
	reader := ioutil.NopCloser(bytes.NewReader(contents))
	yamlDecoder := yaml.NewDocumentDecoder(reader)
	yamlDecoder.Close()
	return nil
}

func init() {
	upgradeCmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade Istio to a new version",
		RunE:  runTestCmd,
	}
	rootCmd.AddCommand(upgradeCmd)
}
