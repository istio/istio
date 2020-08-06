package e2e_taint

import (
	"context"
	"fmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"istio.io/istio/cni/pkg/taint"
	v12 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	client "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const (
	TimeInterval      = "100ms"
	LongTimeDuration  = 10 * time.Second
	ShortTimeDuration = 3 * time.Second
	Daemonset1Name    = "single-critical-label"
	Daemonset2Name    = "addon-in-test2"
	Daemonset3Name    = "addon-in-test1"
)

var (
	Test1NS = v1.Namespace{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test1",
		},
		Spec:   v1.NamespaceSpec{},
		Status: v1.NamespaceStatus{},
	}
	Test2NS = v1.Namespace{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test2",
		},
		Spec:   v1.NamespaceSpec{},
		Status: v1.NamespaceStatus{},
	}
)

// Set up Kubernetes client using kubeconfig (or in-cluster config if no file provided)
func clientSetup() (clientset *client.Clientset, err error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return
	}
	clientset, err = client.NewForConfig(config)
	return
}
func addTaintToAllNodes(taintSetter *taint.TaintSetter) {
	nodeList, err := taintSetter.GetAllNodes()
	if err != nil {
		log.Fatalf("Could not fetch all nodes: %s", err)
	}
	for _, node := range nodeList.Items {
		err = taintSetter.AddReadinessTaint(&node)
		if err != nil {
			log.Fatalf("Could not add taint on node %s, %s", node.Name, err.Error())
		}
	}
}

//read file given location
func buildDaemonSetFromYAML(fileLocation string) (obj v12.DaemonSet, err error) {
	reader, err := os.Open(fileLocation)
	if err != nil {
		log.Fatalf(err.Error())
	}
	obj = v12.DaemonSet{}
	err = yaml.NewYAMLOrJSONDecoder(reader, 4096).Decode(&obj)
	return
}
func createDaemonset(client *client.Interface, fileLocation string) {
	obj, err := buildDaemonSetFromYAML(fileLocation)
	if err != nil {
		log.Fatalf(err.Error())
	}
	_, err = (*client).AppsV1().DaemonSets(obj.Namespace).Create(context.TODO(), &obj, metav1.CreateOptions{})
	if err != nil {
		log.Fatalf(err.Error())
	}
}

//build configmap given name and namespace in cmd.ControllerOptions and location
func buildConfigIfNotExists(clientSet client.Interface, options taint.Options, fileLocation string) (configmap *v1.ConfigMap, err error) {
	err = clientSet.CoreV1().ConfigMaps(options.ConfigNameSpace).Delete(context.TODO(), options.ConfigName, metav1.DeleteOptions{})
	fmt.Println(filepath.Abs("./"))
	currpath, err := filepath.Abs("./")
	cmd := exec.Command("kubectl", "create", "configmap", options.ConfigName, "-n", options.ConfigNameSpace, "--from-file="+fmt.Sprintf("%s\\%s\\", currpath, fileLocation))
	fmt.Println(cmd.String())
	err = cmd.Run()
	configmap, err = clientSet.CoreV1().ConfigMaps(options.ConfigNameSpace).Get(context.TODO(), options.ConfigName, metav1.GetOptions{})
	//spin lock is fine because configmap creation is pretty fast
	for {
		if err == nil {
			return
		} else {
			configmap, err = clientSet.CoreV1().ConfigMaps(options.ConfigNameSpace).Get(context.TODO(), options.ConfigName, metav1.GetOptions{})
		}
	}
}

var _ = Describe("test on multiple labels in two namespace", func() {
	var clientSet client.Interface
	var err error
	var TaintOptions *taint.Options
	BeforeEach(func() {
		time.Sleep(6 * LongTimeDuration)
		//prepare clientset
		clientSet, err = clientSetup()
		if err != nil {
			Fail(err.Error())
		}
		_, err = clientSet.CoreV1().Namespaces().Create(context.TODO(), &Test1NS, metav1.CreateOptions{})
		_, err = clientSet.CoreV1().Namespaces().Create(context.TODO(), &Test2NS, metav1.CreateOptions{})
	})
	AfterEach(func() {
		Eventually(func() bool {
			err = clientSet.CoreV1().ConfigMaps(TaintOptions.ConfigNameSpace).Delete(context.TODO(), TaintOptions.ConfigName, metav1.DeleteOptions{})
			if err != nil {
				return false
			}
			err = clientSet.AppsV1().DaemonSets(Test1NS.Name).Delete(context.TODO(), Daemonset1Name, metav1.DeleteOptions{})
			if err != nil {
				return false
			}
			err = clientSet.CoreV1().Namespaces().Delete(context.TODO(), Test1NS.ObjectMeta.GetName(), metav1.DeleteOptions{})
			if err != nil {
				return false
			}
			err = clientSet.AppsV1().DaemonSets(Test2NS.Name).Delete(context.TODO(), Daemonset2Name, metav1.DeleteOptions{})
			if err != nil {
				return false
			}
			err = clientSet.CoreV1().Namespaces().Delete(context.TODO(), Test2NS.ObjectMeta.GetName(), metav1.DeleteOptions{})
			if err != nil {
				return false
			}
			return true
		}, ShortTimeDuration, TimeInterval).Should(BeTrue())
	})
	It("[taint-controller features]  test on multiple labels in two namespace", func() {
		TaintOptions = &taint.Options{
			ConfigName:      "multi",
			ConfigNameSpace: "default",
		}
		configmap, _ := buildConfigIfNotExists(clientSet, *TaintOptions, "testdata\\multiConfig")
		fmt.Println(configmap.Data)
		taintSetter, err := taint.NewTaintSetter(clientSet, TaintOptions)
		if err != nil {
			log.Fatalf("Could not construct taint setter: %s", err)
		}
		addTaintToAllNodes(&taintSetter)
		tc, err := taint.NewTaintSetterController(&taintSetter)
		if err != nil {
			log.Fatalf("Could not build controller %s", err)
		}
		stopCh := make(chan struct{})
		go tc.Run(stopCh)
		Consistently(func() bool {
			nodeList, err := taintSetter.GetAllNodes()
			if err != nil {
				log.Fatalf("Could not fetch all nodes: %s", err)
			}
			for _, node := range nodeList.Items {
				if !taintSetter.HasReadinessTaint(&node) {
					return false
				}
			}
			return true
		}, ShortTimeDuration, TimeInterval).Should(BeTrue())
		time.Sleep(ShortTimeDuration)
		//create sample file
		createDaemonset(&taintSetter.Client, "testdata/podsConfig/single-critical-label.yaml")
		//it will not be ready because some labels in the namespace are not satisfied
		Consistently(func() bool {
			nodeList, err := taintSetter.GetAllNodes()
			if err != nil {
				log.Fatalf("Could not fetch all nodes: %s", err)
			}
			for _, node := range nodeList.Items {
				if !taintSetter.HasReadinessTaint(&node) {
					return false
				}
			}
			return true
		}, ShortTimeDuration, TimeInterval).Should(BeTrue())
		time.Sleep(ShortTimeDuration)
		createDaemonset(&taintSetter.Client, "testdata/podsConfig/addon-test2-in-test2.yaml")
		//eventually all node's label should be removed because critical labels are satisfied
		Eventually(func() bool {
			nodes, _ := taintSetter.GetAllNodes()
			for _, node := range nodes.Items {
				if taintSetter.HasReadinessTaint(&node) {
					return false
				}
			}
			return true
		}, LongTimeDuration, TimeInterval).Should(BeTrue())
		close(stopCh)
	})
})
var _ = Describe("[taint-controller features] single case", func() {
	var clientSet client.Interface
	var err error
	var TaintOptions *taint.Options
	BeforeEach(func() {
		time.Sleep(6 * LongTimeDuration)
		//prepare clientset
		clientSet, err = clientSetup()
		if err != nil {
			Fail(err.Error())
		}
		_, err = clientSet.CoreV1().Namespaces().Create(context.TODO(), &Test1NS, metav1.CreateOptions{})
	})
	It("test on single label case", func() {
		TaintOptions = &taint.Options{
			ConfigName:      "single",
			ConfigNameSpace: "default",
		}
		_, _ = buildConfigIfNotExists(clientSet, *TaintOptions, "testdata\\singleConfig")
		taintSetter, err := taint.NewTaintSetter(clientSet, TaintOptions)
		if err != nil {
			log.Fatalf("Could not construct taint setter: %s", err)
		}
		addTaintToAllNodes(&taintSetter)
		tc, err := taint.NewTaintSetterController(&taintSetter)
		if err != nil {
			log.Fatalf("Could not build controller %s", err)
		}
		stopCh := make(chan struct{})
		go tc.Run(stopCh)
		Consistently(func() bool {
			nodeList, err := taintSetter.GetAllNodes()
			if err != nil {
				log.Fatalf("Could not fetch all nodes: %s", err)
			}
			for _, node := range nodeList.Items {
				if !taintSetter.HasReadinessTaint(&node) {
					return false
				}
			}
			return true
		}, ShortTimeDuration, TimeInterval).Should(BeTrue())
		time.Sleep(ShortTimeDuration)
		createDaemonset(&taintSetter.Client, "testdata/podsConfig/single-critical-label.yaml")
		//eventually all node's label should be removed because critical labels are satisfied
		Eventually(func() bool {
			nodes, _ := taintSetter.GetAllNodes()
			for _, node := range nodes.Items {
				if taintSetter.HasReadinessTaint(&node) {
					return false
				}
			}
			return true
		}, LongTimeDuration, TimeInterval).Should(BeTrue())
		close(stopCh)
	})
	AfterEach(func() {
		Eventually(func() bool {
			err = clientSet.CoreV1().ConfigMaps(TaintOptions.ConfigNameSpace).Delete(context.TODO(), TaintOptions.ConfigName, metav1.DeleteOptions{})
			if err != nil {
				return false
			}
			err = clientSet.AppsV1().DaemonSets(Test1NS.Name).Delete(context.TODO(), Daemonset1Name, metav1.DeleteOptions{})
			if err != nil {
				return false
			}
			err = clientSet.CoreV1().Namespaces().Delete(context.TODO(), Test1NS.ObjectMeta.GetName(), metav1.DeleteOptions{})
			if err != nil {
				return false
			}
			return true
		}, ShortTimeDuration, TimeInterval).Should(BeTrue())
	})

})
var _ = Describe("[taint-controller features]  multi Label in one case", func() {
	var clientSet client.Interface
	var err error
	var TaintOptions *taint.Options
	BeforeEach(func() {
		time.Sleep(6 * LongTimeDuration)
		//prepare clientset
		clientSet, err = clientSetup()
		if err != nil {
			Fail(err.Error())
		}
		_, err = clientSet.CoreV1().Namespaces().Create(context.TODO(), &Test1NS, metav1.CreateOptions{})
	})
	It("test on multi label in one case", func() {
		TaintOptions = &taint.Options{
			ConfigName:      "mutli.one",
			ConfigNameSpace: "default",
		}
		_, _ = buildConfigIfNotExists(clientSet, *TaintOptions, "testdata/multiInOneConfig")
		taintSetter, err := taint.NewTaintSetter(clientSet, TaintOptions)
		if err != nil {
			log.Fatalf("Could not construct taint setter: %s", err)
		}
		addTaintToAllNodes(&taintSetter)
		tc, err := taint.NewTaintSetterController(&taintSetter)
		if err != nil {
			log.Fatalf("Could not build controller %s", err)
		}
		stopCh := make(chan struct{})
		go tc.Run(stopCh)
		Consistently(func() bool {
			nodeList, err := taintSetter.GetAllNodes()
			if err != nil {
				log.Fatalf("Could not fetch all nodes: %s", err)
			}
			for _, node := range nodeList.Items {
				if !taintSetter.HasReadinessTaint(&node) {
					return false
				}
			}
			return true
		}, ShortTimeDuration, TimeInterval).Should(BeTrue())
		time.Sleep(ShortTimeDuration)
		createDaemonset(&taintSetter.Client, "testdata/podsConfig/single-critical-label.yaml")
		Consistently(func() bool {
			nodeList, err := taintSetter.GetAllNodes()
			if err != nil {
				log.Fatalf("Could not fetch all nodes: %s", err)
			}
			for _, node := range nodeList.Items {
				if !taintSetter.HasReadinessTaint(&node) {
					return false
				}
			}
			return true
		}, ShortTimeDuration, TimeInterval).Should(BeTrue())
		createDaemonset(&taintSetter.Client, "testdata/podsConfig/addon-test1.yaml")
		//eventually all node's label should be removed because critical labels are satisfied
		Eventually(func() bool {
			nodes, _ := taintSetter.GetAllNodes()
			for _, node := range nodes.Items {
				if taintSetter.HasReadinessTaint(&node) {
					return false
				}
			}
			return true
		}, LongTimeDuration, TimeInterval).Should(BeTrue())
		close(stopCh)
	})
	AfterEach(func() {
		Eventually(func() bool {
			err = clientSet.CoreV1().ConfigMaps(TaintOptions.ConfigNameSpace).Delete(context.TODO(), TaintOptions.ConfigName, metav1.DeleteOptions{})
			if err != nil {
				return false
			}
			err = clientSet.AppsV1().DaemonSets(Test1NS.Name).Delete(context.TODO(), Daemonset1Name, metav1.DeleteOptions{})
			if err != nil {
				return false
			}
			err = clientSet.AppsV1().DaemonSets(Test1NS.Name).Delete(context.TODO(), Daemonset3Name, metav1.DeleteOptions{})
			if err != nil {
				return false
			}
			err = clientSet.CoreV1().Namespaces().Delete(context.TODO(), Test1NS.ObjectMeta.GetName(), metav1.DeleteOptions{})
			if err != nil {
				return false
			}
			return true
		}, LongTimeDuration, TimeInterval).Should(BeTrue())
	})

})

func TestTaintController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "testing general behaeviors in taint controller with kind or minikube cluster")
}
