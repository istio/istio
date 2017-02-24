// Copyright 2017 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	meta_v1 "k8s.io/client-go/pkg/apis/meta/v1"

	flag "github.com/spf13/pflag"

	"istio.io/manager/model"
	"istio.io/manager/platform/kube"
)

const (
	managerDiscovery     = "manager-discovery"
	mixer                = "mixer"
	egressProxy          = "egress-proxy"
	app                  = "app"
	appProxyManagerAgent = "app-proxy-manager-agent"

	// budget is the maximum number of retries with 1s delays
	budget = 30

	// Mixer SHA *update manually*
	mixerSHA = "ea3a8d3e2feb9f06256f92cda5194cc1ea6b599e"
)

// params contain default template parameter values
type parameters struct {
	hub        string
	tag        string
	mixerImage string
	namespace  string
}

var (
	params parameters

	kubeconfig string

	verbose  bool
	parallel bool

	client      *kubernetes.Clientset
	istioClient *kube.Client

	// pods is a mapping from app name to a pod name (write once, read only)
	pods map[string]string

	// indicates whether the namespace is auto-generated
	nameSpaceCreated bool
)

func init() {
	flag.StringVarP(&params.hub, "hub", "h", "gcr.io/istio-testing",
		"Docker hub")
	flag.StringVarP(&params.tag, "tag", "t", "",
		"Docker tag")
	flag.StringVar(&params.mixerImage, "mixerImage", "gcr.io/istio-testing/mixer:"+mixerSHA,
		"Mixer Docker image")
	flag.StringVarP(&params.namespace, "namespace", "n", "",
		"Namespace to use for testing (empty to create/delete temporary one)")
	flag.StringVarP(&kubeconfig, "config", "c", "platform/kube/config",
		"kube config file (missing or empty file makes the test use in-cluster kube config instead)")
	flag.BoolVarP(&verbose, "dump", "d", false,
		"Dump proxy logs and request logs")
	flag.BoolVar(&parallel, "parallel", true,
		"Run requests in parallel")
}

func main() {
	flag.Parse()
	setup()
	check((&reachability{}).run())
	check(testRouting())
	teardown()
}

func setup() {
	if params.tag == "" {
		log.Fatal("No docker tag specified with -t or --tag")
	}
	if params.mixerImage == "" {
		log.Fatal("No mixer image specified with --mixerImage, 'latest?'")
	}
	log.Printf("params %#v", params)

	check(setupClient())

	var err error
	if params.namespace == "" {
		if params.namespace, err = generateNamespace(client); err != nil {
			check(err)
		}
	} else {
		_, err = client.Core().Namespaces().Get(params.namespace, meta_v1.GetOptions{})
		check(err)
	}

	pods = make(map[string]string)

	// deploy istio-infra
	check(deploy("http-discovery", "http-discovery", managerDiscovery, "8080", "80", "unversioned"))
	check(deploy("mixer", "mixer", mixer, "8080", "80", "unversioned"))
	check(deploy("istio-egress", "istio-egress", egressProxy, "8080", "80", "unversioned"))

	// deploy a healthy mix of apps, with and without proxy
	check(deploy("t", "t", app, "8080", "80", "unversioned"))
	check(deploy("a", "a", appProxyManagerAgent, "8080", "80", "unversioned"))
	check(deploy("b", "b", appProxyManagerAgent, "80", "8080", "unversioned"))
	check(deploy("hello", "hello", appProxyManagerAgent, "8080", "80", "v1"))
	check(deploy("world-v1", "world", appProxyManagerAgent, "80", "8000", "v1"))
	check(deploy("world-v2", "world", appProxyManagerAgent, "80", "8000", "v2"))
	check(setPods())
}

// check function correctly cleans up on failure
func check(err error) {
	if err != nil {
		log.Print(err)
		teardown()
		os.Exit(1)
	}
}

// teardown removes resources
func teardown() {
	if verbose {
		log.Print(podLogs(pods["a"], "proxy"))
	}
	if nameSpaceCreated {
		deleteNamespace(client, params.namespace)
		params.namespace = ""
	}
}

func deploy(name, svcName, dType, port1, port2, version string) error {
	// write template
	configFile := name + "-" + dType + ".yaml"
	var w *bufio.Writer
	f, err := os.Create(configFile)
	if err != nil {
		return err
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("Error closing file " + configFile)
		}
	}()

	w = bufio.NewWriter(f)

	if err := write("test/integration/"+dType+".yaml.tmpl", map[string]string{
		"service": svcName,
		"name":    name,
		"port1":   port1,
		"port2":   port2,
		"version": version,
	}, w); err != nil {
		return err
	}

	if err := w.Flush(); err != nil {
		return err
	}

	return run("kubectl apply -f " + configFile + " -n " + params.namespace)
}

func waitForNewRestartEpoch(pod string, start int) error {
	log.Println("Waiting for Envoy restart epoch to increment...")
	for n := 0; n < budget; n++ {
		current, err := getRestartEpoch(pod)
		if err != nil {
			log.Printf("Could not obtain Envoy restart epoch for %s: %v", pod, err)
		}

		if current > start {
			return nil
		}
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("exceeded budget for waiting for envoy restart epoch to increment")
}

// getRestartEpoch gets the current restart epoch of a pod by calling the Envoy admin API.
func getRestartEpoch(pod string) (int, error) {
	url := "http://localhost:5000/server_info"
	cmd := fmt.Sprintf("kubectl exec %s -n %s -c app client %s", pods[pod], params.namespace, url)
	out, err := shell(cmd, true)
	if err != nil {
		return 0, err
	}

	// Response body is of the form: envoy 267724/RELEASE live 1571 1571 0
	// The last value is the restart epoch.
	match := regexp.MustCompile(`envoy .+/\w+ \w+ \d+ \d+ (\d+)`).FindStringSubmatch(out)
	if len(match) > 1 {
		epoch, err := strconv.ParseInt(match[1], 10, 32)
		return int(epoch), err
	}

	return 0, fmt.Errorf("could not obtain envoy restart epoch")
}

func addConfig(config []byte, kind, name string, create bool) {
	log.Println("Add config")
	log.Println(string(config))
	istioKind, ok := model.IstioConfig[kind]
	if !ok {
		check(fmt.Errorf("Invalid kind %s", kind))
	}
	v, err := istioKind.FromYAML(string(config))
	check(err)
	key := model.Key{
		Kind:      kind,
		Name:      name,
		Namespace: params.namespace,
	}
	if create {
		check(istioClient.Post(key, v))
	} else {
		check(istioClient.Put(key, v))
	}
}

func deployConfig(in string, data map[string]string, kind, name string, envoy string) {
	config, err := writeString(in, data)
	check(err)
	epoch, err := getRestartEpoch(envoy)
	check(err)
	_, exists := istioClient.Get(model.Key{Kind: kind, Name: name, Namespace: params.namespace})
	addConfig(config, kind, name, !exists)
	check(waitForNewRestartEpoch(envoy, epoch))
}

func write(in string, data map[string]string, out io.Writer) error {
	// fallback to params values in data
	values := make(map[string]string)
	values["hub"] = params.hub
	values["tag"] = params.tag
	values["mixerImage"] = params.mixerImage
	values["namespace"] = params.namespace
	for k, v := range data {
		values[k] = v
	}
	tmpl, err := template.ParseFiles(in)

	if err != nil {
		return err
	}
	if err := tmpl.Execute(out, values); err != nil {
		return err
	}
	return nil
}

func writeString(in string, data map[string]string) ([]byte, error) {
	var bytes bytes.Buffer
	w := bufio.NewWriter(&bytes)
	if err := write(in, data, w); err != nil {
		return nil, err
	}

	if err := w.Flush(); err != nil {
		return nil, err
	}

	return bytes.Bytes(), nil
}

func run(command string) error {
	log.Println(command)
	parts := strings.Split(command, " ")
	/* #nosec */
	c := exec.Command(parts[0], parts[1:]...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func shell(command string, printCmd bool) (string, error) {
	if printCmd {
		log.Println(command)
	}
	parts := strings.Split(command, " ")
	/* #nosec */
	c := exec.Command(parts[0], parts[1:]...)
	bytes, err := c.CombinedOutput()
	if err != nil {
		log.Println(string(bytes))
		return "", fmt.Errorf("command failed: %q %v", string(bytes), err)
	}
	return string(bytes), nil
}

// connect to K8S cluster and register TPRs
func setupClient() error {
	var err error
	istioClient, err = kube.NewClient(kubeconfig, model.IstioConfig)
	if err != nil {
		return err
	}
	client = istioClient.GetKubernetesClient()
	return istioClient.RegisterResources()
}

func setPods() error {
	items := make([]v1.Pod, 0)
	for n := 0; ; n++ {
		log.Println("Checking all pods are running...")
		list, err := client.Pods(params.namespace).List(v1.ListOptions{})
		if err != nil {
			return err
		}
		items = list.Items
		ready := true

		for _, pod := range items {
			if pod.Status.Phase != "Running" {
				log.Printf("Pod %s has status %s\n", pod.Name, pod.Status.Phase)
				ready = false
				break
			}
		}

		if ready {
			break
		}

		if n > budget {
			for _, pod := range items {
				log.Print(podLogs(pod.Name, "proxy"))
			}
			return fmt.Errorf("exceeded budget for checking pod status")
		}

		time.Sleep(time.Second)
	}

	for _, pod := range items {
		if app, exists := pod.Labels["app"]; exists {
			pods[app] = pod.Name
		}
	}

	return nil
}

// podLogs gets pod logs by container
func podLogs(name string, container string) string {
	log.Println("Pod proxy logs", name)
	raw, err := client.Pods(params.namespace).
		GetLogs(name, &v1.PodLogOptions{Container: container}).
		Do().Raw()
	if err != nil {
		log.Println("Request error", err)
		return ""
	}

	return string(raw)
}

func generateNamespace(cl *kubernetes.Clientset) (string, error) {
	ns, err := cl.Core().Namespaces().Create(&v1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			GenerateName: "istio-integration-",
		},
	})
	if err != nil {
		return "", err
	}
	log.Printf("Created namespace %s\n", ns.Name)
	nameSpaceCreated = true
	return ns.Name, nil
}

func deleteNamespace(cl *kubernetes.Clientset, ns string) {
	if cl != nil && ns != "" && ns != "default" {
		if err := cl.Core().Namespaces().Delete(ns, &v1.DeleteOptions{}); err != nil {
			log.Printf("Error deleting namespace: %v\n", err)
		}
		log.Printf("Deleted namespace %s\n", ns)
	}
}
