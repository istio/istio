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

package util

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/ghodss/yaml"
	"github.com/golang/sync/errgroup"
	multierror "github.com/hashicorp/go-multierror"
	"golang.org/x/net/context/ctxhttp"
	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/cluster-registry/pkg/apis/clusterregistry/v1alpha1"

	"istio.io/istio/pkg/log"
)

const (
	podFailedGet = "Failed_Get"
	// The index of STATUS field in kubectl CLI output.
	statusField          = 2
	defaultClusterSubnet = "24"

	// NodePortServiceType NodePort type of Kubernetes Service
	NodePortServiceType = "NodePort"

	// LoadBalancerServiceType LoadBalancer type of Kubernetes Service
	LoadBalancerServiceType = "LoadBalancer"
)

var (
	logDumpResources = []string{
		"pod",
		"service",
		"ingress",
	}
)

// Fill complete a template with given values and generate a new output file
func Fill(outFile, inFile string, values interface{}) error {
	tmpl, err := template.ParseFiles(inFile)
	if err != nil {
		return err
	}

	var filled bytes.Buffer
	w := bufio.NewWriter(&filled)
	if err := tmpl.Execute(w, values); err != nil {
		return err
	}

	if err := w.Flush(); err != nil {
		return err
	}

	if err := ioutil.WriteFile(outFile, filled.Bytes(), 0644); err != nil {
		return err
	}
	log.Infof("Created %s from template %s", outFile, inFile)
	return nil
}

// CreateAndFill fills in the given yaml template with the values and generates a temp file for the completed yaml.
func CreateAndFill(outDir, templateFile string, values interface{}) (string, error) {
	outFile, err := CreateTempfile(outDir, filepath.Base(templateFile), "yaml")
	if err != nil {
		log.Errorf("Failed to generate yaml %s: %v", templateFile, err)
		return "", err
	}
	if err := Fill(outFile, templateFile, values); err != nil {
		log.Errorf("Failed to generate yaml for template %s", templateFile)
		return "", err
	}
	return outFile, nil
}

// CreateNamespace create a kubernetes namespace
func CreateNamespace(n string, kubeconfig string) error {
	if _, err := ShellMuteOutput("kubectl create namespace %s --kubeconfig=%s", n, kubeconfig); err != nil {
		if !strings.Contains(err.Error(), "AlreadyExists") {
			return err
		}
	}
	log.Infof("namespace %s created\n", n)
	return nil
}

// DeleteNamespace delete a kubernetes namespace
func DeleteNamespace(n string, kubeconfig string) error {
	_, err := Shell("kubectl delete namespace %s --kubeconfig=%s", n, kubeconfig)
	return err
}

// DeleteDeployment deletes deployment from the specified namespace
func DeleteDeployment(d string, n string, kubeconfig string) error {
	_, err := Shell("kubectl delete deployment %s -n %s --kubeconfig=%s", d, n, kubeconfig)
	return err
}

// NamespaceDeleted check if a kubernete namespace is deleted
func NamespaceDeleted(n string, kubeconfig string) (bool, error) {
	output, err := ShellSilent("kubectl get namespace %s -o name --kubeconfig=%s", n, kubeconfig)
	if strings.Contains(output, "NotFound") {
		return true, nil
	}
	return false, err
}

// KubeApplyContents kubectl apply from contents
func KubeApplyContents(namespace, yamlContents string, kubeconfig string) error {
	tmpfile, err := WriteTempfile(os.TempDir(), "kubeapply", ".yaml", yamlContents)
	if err != nil {
		return err
	}
	defer removeFile(tmpfile)
	return KubeApply(namespace, tmpfile, kubeconfig)
}

func kubeCommand(subCommand, namespace, yamlFileName string, kubeconfig string) string {
	if namespace == "" {
		return fmt.Sprintf("kubectl %s -f %s --kubeconfig=%s", subCommand, yamlFileName, kubeconfig)
	}
	return fmt.Sprintf("kubectl %s -n %s -f %s --kubeconfig=%s", subCommand, namespace, yamlFileName, kubeconfig)
}

// KubeApply kubectl apply from file
func KubeApply(namespace, yamlFileName string, kubeconfig string) error {
	_, err := Shell(kubeCommand("apply", namespace, yamlFileName, kubeconfig))
	return err
}

// KubeApplyContentSilent kubectl apply from contents silently
func KubeApplyContentSilent(namespace, yamlContents string, kubeconfig string) error {
	tmpfile, err := WriteTempfile(os.TempDir(), "kubeapply", ".yaml", yamlContents)
	if err != nil {
		return err
	}
	defer removeFile(tmpfile)
	return KubeApplySilent(namespace, tmpfile, kubeconfig)
}

// KubeApplySilent kubectl apply from file silently
func KubeApplySilent(namespace, yamlFileName string, kubeconfig string) error {
	_, err := ShellSilent(kubeCommand("apply", namespace, yamlFileName, kubeconfig))
	return err
}

// HelmInit init helm with a service account
func HelmInit(serviceAccount string) error {
	_, err := Shell("helm init --upgrade --service-account %s", serviceAccount)
	return err
}

// HelmInstallDryRun helm install dry run from a chart for a given namespace
func HelmInstallDryRun(chartDir, chartName, namespace, setValue string) error {
	_, err := Shell("helm install --dry-run --debug %s --name %s --namespace %s %s", chartDir, chartName, namespace, setValue)
	return err
}

// HelmInstall helm install from a chart for a given namespace
//       --set stringArray        set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)
func HelmInstall(chartDir, chartName, namespace, setValue string) error {
	_, err := Shell("helm install %s --name %s --namespace %s %s", chartDir, chartName, namespace, setValue)
	return err
}

// HelmDelete helm del --purge a chart
func HelmDelete(chartName string) error {
	_, err := Shell("helm del --purge %s", chartName)
	return err
}

// KubeDeleteContents kubectl apply from contents
func KubeDeleteContents(namespace, yamlContents string, kubeconfig string) error {
	tmpfile, err := WriteTempfile(os.TempDir(), "kubedelete", ".yaml", yamlContents)
	if err != nil {
		return err
	}
	defer removeFile(tmpfile)
	return KubeDelete(namespace, tmpfile, kubeconfig)
}

func removeFile(path string) {
	err := os.Remove(path)
	if err != nil {
		log.Errorf("Unable to remove %s: %v", path, err)
	}
}

// KubeDelete kubectl delete from file
func KubeDelete(namespace, yamlFileName string, kubeconfig string) error {
	_, err := Shell(kubeCommand("delete", namespace, yamlFileName, kubeconfig))
	return err
}

// GetKubeMasterIP returns the IP address of the kubernetes master service.
// TODO update next 2 func to pass in the kubeconfig
func GetKubeMasterIP() (string, error) {
	return ShellSilent("kubectl get svc kubernetes -n default -o jsonpath='{.spec.clusterIP}'")
}

// GetClusterSubnet returns the subnet (in CIDR form, e.g. "24") for the nodes in the cluster.
func GetClusterSubnet() (string, error) {
	cidr, err := ShellSilent("kubectl get nodes -o jsonpath='{.items[0].spec.podCIDR}'")
	if err != nil {
		// This command should never fail. If the field isn't found, it will just return and empty string.
		return "", err
	}
	parts := strings.Split(cidr, "/")
	if len(parts) != 2 {
		// TODO(nmittler): Need a way to get the subnet on minikube. For now, just return a default value.
		log.Info("unable to identify cluster subnet. running on minikube?")
		return defaultClusterSubnet, nil
	}
	return parts[1], nil
}

func getRetrier(serviceType string) Retrier {
	baseDelay := 1 * time.Second
	maxDelay := 1 * time.Second
	retries := 300 // ~5 minutes

	if serviceType == NodePortServiceType {
		baseDelay = 5 * time.Second
		maxDelay = 5 * time.Second
		retries = 20
	}

	return Retrier{
		BaseDelay: baseDelay,
		MaxDelay:  maxDelay,
		Retries:   retries,
	}
}

// GetIngress get istio ingress ip and port. Could relate to either Istio Ingress or to
// Istio Ingress Gateway, by serviceName and podLabel. Handles two cases: when the Ingress/Ingress Gateway
// Kubernetes Service is a LoadBalancer or NodePort (for tests within the  cluster, including for minikube)
func GetIngress(serviceName, podLabel, namespace, kubeconfig string, serviceType string) (string, error) {

	retry := getRetrier(serviceType)
	var ingress string
	retryFn := func(_ context.Context, i int) error {
		var err error

		if serviceType == NodePortServiceType {
			ingress, err = getServiceNodePort(serviceName, podLabel, namespace, kubeconfig)
		} else if serviceType == LoadBalancerServiceType {
			ingress, err = getServiceLoadBalancer(serviceName, namespace, kubeconfig)
		} else {
			return fmt.Errorf("unknown service type: %s", serviceType)
		}

		if err != nil {
			return err
		}
		log.Infof("Istio ingress: %s", ingress)
		return nil
	}

	ctx := context.Background()

	log.Infof("Waiting for %s to get external IP", serviceName)
	if _, err := retry.Retry(ctx, retryFn); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()

	client := &http.Client{Timeout: 5 * time.Second}
	ingressURL := fmt.Sprintf("http://%s", ingress)
	log.Infof("Sanity checking %v", ingressURL)
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("%s readiness check timed out", serviceName)
		default:
			response, err := ctxhttp.Get(ctx, client, ingressURL)
			if err == nil {
				log.Infof("Response %v %q received from %v", response.StatusCode, response.Status, ingressURL)
				return ingress, nil
			}
		}
	}
}

func getServiceLoadBalancer(name, namespace, kubeconfig string) (string, error) {
	ip, err := ShellSilent(
		"kubectl get svc %s -n %s -o jsonpath='{.status.loadBalancer.ingress[*].ip}' --kubeconfig=%s",
		name, namespace, kubeconfig)

	if err != nil {
		return "", err
	}

	ip = strings.Trim(ip, "'")
	ri := regexp.MustCompile(`^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$`)
	if ri.FindString(ip) == "" {
		return "", errors.New("ingress ip not available yet")
	}

	return ip, nil
}

func getServiceNodePort(serviceName, podLabel, namespace, kubeconfig string) (string, error) {
	ip, err := Shell(
		"kubectl get po -l istio=%s -n %s -o jsonpath='{.items[0].status.hostIP}' --kubeconfig=%s",
		podLabel, namespace, kubeconfig)

	if err != nil {
		return "", err
	}

	ip = strings.Trim(ip, "'")
	ri := regexp.MustCompile(`^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$`)
	if ri.FindString(ip) == "" {
		return "", fmt.Errorf("the ip of %s is not available yet", serviceName)
	}

	port, err := getServicePort(serviceName, namespace, kubeconfig)
	if err != nil {
		return "", err
	}

	return ip + ":" + port, nil
}

func getServicePort(serviceName, namespace, kubeconfig string) (string, error) {
	port, err := Shell(
		"kubectl get svc %s -n %s -o jsonpath='{.spec.ports[0].nodePort}' --kubeconfig=%s",
		serviceName, namespace, kubeconfig)

	if err != nil {
		return "", err
	}

	port = strings.Trim(port, "'")
	rp := regexp.MustCompile(`^[0-9]{1,5}$`)
	if rp.FindString(port) == "" {
		err = fmt.Errorf("unable to find the port of %s", serviceName)
		log.Warna(err)
		return "", err
	}
	return port, nil
}

// GetIngressPodNames get the pod names for the Istio ingress deployment.
func GetIngressPodNames(n string, kubeconfig string) ([]string, error) {
	res, err := Shell("kubectl get pod -l istio=ingress -n %s -o jsonpath='{.items[*].metadata.name}' --kubeconfig=%s", n, kubeconfig)
	if err != nil {
		return nil, err
	}
	res = strings.Trim(res, "'")
	return strings.Split(res, " "), nil
}

// GetAppPods gets a map of app names to the pods for the app, for the given namespace
func GetAppPods(n string, kubeconfig string) (map[string][]string, error) {
	podLabels, err := GetPodLabelValues(n, "app", kubeconfig)
	if err != nil {
		return nil, err
	}

	m := make(map[string][]string)
	for podName, app := range podLabels {
		m[app] = append(m[app], podName)
	}
	return m, nil
}

// GetPodLabelValues gets a map of pod name to label value for the given label and namespace
func GetPodLabelValues(n, label string, kubeconfig string) (map[string]string, error) {
	// This will return a table where c0=pod_name and c1=label_value.
	// The columns are separated by a space and each result is on a separate line (separated by '\n').
	res, err := Shell("kubectl -n %s -l=%s get pods -o=jsonpath='{range .items[*]}{.metadata.name}{\" \"}{"+
		".metadata.labels.%s}{\"\\n\"}{end}' --kubeconfig=%s", n, label, label, kubeconfig)
	if err != nil {
		log.Infof("Failed to get pods by label %s in namespace %s: %s", label, n, err)
		return nil, err
	}

	// Split the lines in the result
	m := make(map[string]string)
	for _, line := range strings.Split(res, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 {
			m[f[0]] = f[1]
		}
	}

	return m, nil
}

// GetPodNames gets names of all pods in specific namespace and return in a slice
func GetPodNames(n string) (pods []string, kubeconfig string) {
	res, err := Shell("kubectl -n %s get pods -o jsonpath='{.items[*].metadata.name}' --kubeconfig=%s", n, kubeconfig)
	if err != nil {
		log.Infof("Failed to get pods name in namespace %s: %s", n, err)
		return
	}
	res = strings.Trim(res, "'")
	pods = strings.Split(res, " ")
	log.Infof("Existing pods: %v", pods)
	return
}

// GetPodStatus gets status of a pod from a namespace
// Note: It is not enough to check pod phase, which only implies there is at
// least one container running. Use kubectl CLI to get status so that we can
// ensure that all containers are running.
func GetPodStatus(n, pod string, kubeconfig string) string {
	status, err := Shell("kubectl -n %s get pods %s --no-headers --kubeconfig=%s", n, pod, kubeconfig)
	if err != nil {
		log.Infof("Failed to get status of pod %s in namespace %s: %s", pod, n, err)
		status = podFailedGet
	}
	f := strings.Fields(status)
	if len(f) > statusField {
		return f[statusField]
	}
	return ""
}

// GetPodName gets the pod name for the given namespace and label selector
func GetPodName(n, labelSelector string, kubeconfig string) (pod string, err error) {
	pod, err = Shell("kubectl -n %s get pod -l %s -o jsonpath='{.items[0].metadata.name}' --kubeconfig=%s", n, labelSelector, kubeconfig)
	if err != nil {
		log.Warnf("could not get %s pod: %v", labelSelector, err)
		return
	}
	pod = strings.Trim(pod, "'")
	log.Infof("%s pod name: %s", labelSelector, pod)
	return
}

// GetPodLogsForLabel gets the logs for the given label selector and container
func GetPodLogsForLabel(n, labelSelector string, container string, tail, alsoShowPreviousPodLogs bool, kubeconfig string) string {
	pod, err := GetPodName(n, labelSelector, kubeconfig)
	if err != nil {
		return ""
	}
	return GetPodLogs(n, pod, container, tail, alsoShowPreviousPodLogs, kubeconfig)
}

// GetPodLogs retrieves the logs for the given namespace, pod and container.
func GetPodLogs(n, pod, container string, tail, alsoShowPreviousPodLogs bool, kubeconfig string) string {
	tailOption := ""
	if tail {
		tailOption = "--tail=40"
	}
	o1 := ""
	if alsoShowPreviousPodLogs {
		log.Info("Expect and ignore an error getting crash logs when there are no crash (-p invocation)")
		// Do not use Shell. It dumps the entire log on the console and makes the test unusable due to very large amount of output
		o1, _ = ShellMuteOutput("kubectl --namespace %s logs %s -c %s %s -p --kubeconfig=%s", n, pod, container, tailOption, kubeconfig)
		o1 += "\n"
	}
	// Do not use Shell. It dumps the entire log on the console and makes the test unusable due to very large amount of output
	o2, _ := ShellMuteOutput("kubectl --namespace %s logs %s -c %s %s --kubeconfig=%s", n, pod, container, tailOption, kubeconfig)
	return o1 + o2
}

// GetConfigs retrieves the configurations for the list of resources.
func GetConfigs(kubeconfig string, names ...string) (string, error) {
	cmd := fmt.Sprintf("kubectl get %s --all-namespaces -o yaml --kubeconfig=%s", strings.Join(names, ","), kubeconfig)
	return Shell(cmd)
}

// PodExec runs the specified command on the container for the specified namespace and pod
func PodExec(n, pod, container, command string, muteOutput bool, kubeconfig string) (string, error) {
	if muteOutput {
		return ShellSilent("kubectl exec --kubeconfig=%s %s -n %s -c %s -- %s", kubeconfig, pod, n, container, command)
	}
	return Shell("kubectl exec --kubeconfig=%s %s -n %s -c %s -- %s ", kubeconfig, pod, n, container, command)
}

// CreateTLSSecret creates a secret from the provided cert and key files
func CreateTLSSecret(secretName, n, keyFile, certFile string, kubeconfig string) (string, error) {
	//cmd := fmt.Sprintf("kubectl create secret tls %s -n %s --key %s --cert %s", secretName, n, keyFile, certFile)
	//return Shell(cmd)
	return Shell("kubectl create secret tls %s -n %s --key %s --cert %s --kubeconfig=%s", secretName, n, keyFile, certFile, kubeconfig)
}

// CheckPodsRunningWithMaxDuration returns if all pods in a namespace are in "Running" status
// Also check container status to be running.
func CheckPodsRunningWithMaxDuration(n string, maxDuration time.Duration, kubeconfig string) (ready bool) {
	if err := WaitForDeploymentsReady(n, maxDuration, kubeconfig); err != nil {
		log.Errorf("CheckPodsRunning: %v", err.Error())
		return false
	}

	return true
}

// CheckPodsRunning returns readiness of all pods within a namespace. It will wait for upto 2 mins.
// use WithMaxDuration to specify a duration.
func CheckPodsRunning(n string, kubeconfig string) (ready bool) {
	return CheckPodsRunningWithMaxDuration(n, 2*time.Minute, kubeconfig)
}

// CheckDeployment gets status of a deployment from a namespace
func CheckDeployment(ctx context.Context, namespace, deployment string, kubeconfig string) error {
	if deployment == "deployments/istio-sidecar-injector" {
		// This can be deployed by previous tests, but doesn't complete currently, blocking the test.
		return nil
	}
	errc := make(chan error)
	go func() {
		if _, err := ShellMuteOutput("kubectl -n %s rollout status %s --kubeconfig=%s", namespace, deployment, kubeconfig); err != nil {
			errc <- fmt.Errorf("%s in namespace %s failed", deployment, namespace)
		}
		errc <- nil
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// CheckDeployments checks whether all deployment in a given namespace
func CheckDeployments(namespace string, timeout time.Duration, kubeconfig string) error {
	// wait for istio-system deployments to be fully rolled out before proceeding
	out, err := Shell("kubectl -n %s get deployment -o name --kubeconfig=%s", namespace, kubeconfig)
	if err != nil {
		return fmt.Errorf("could not list deployments in namespace %q", namespace)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	g, ctx := errgroup.WithContext(ctx)
	deployments := strings.Fields(out)
	for i := range deployments {
		deployment := deployments[i]
		g.Go(func() error { return CheckDeployment(ctx, namespace, deployment, kubeconfig) })
	}
	return g.Wait()
}

// FetchAndSaveClusterLogs will dump the logs for a cluster.
func FetchAndSaveClusterLogs(namespace string, tempDir string, kubeconfig string) error {
	var multiErr error
	fetchAndWrite := func(pod string) error {
		cmd := fmt.Sprintf(
			"kubectl get pods -n %s %s -o jsonpath={.spec.containers[*].name} --kubeconfig=%s", namespace, pod, kubeconfig)
		containersString, err := Shell(cmd)
		if err != nil {
			return err
		}
		containers := strings.Split(containersString, " ")
		for _, container := range containers {
			filePath := filepath.Join(tempDir, fmt.Sprintf("%s_container:%s.log", pod, container))
			f, err := os.Create(filePath)
			if err != nil {
				return err
			}
			defer func() {
				if err = f.Close(); err != nil {
					log.Warnf("Error during closing file: %v\n", err)
				}
			}()
			dump, err := ShellMuteOutput(
				fmt.Sprintf("kubectl logs %s -n %s -c %s --kubeconfig=%s", pod, namespace, container, kubeconfig))
			if err != nil {
				return err
			}

			if _, err = f.WriteString(fmt.Sprintf("%s\n", dump)); err != nil {
				return err
			}

			dump1, err := ShellMuteOutput(
				fmt.Sprintf("kubectl logs %s -n %s -c %s -p --kubeconfig=%s", pod, namespace, container, kubeconfig))
			if err != nil {
				log.Infof("No previous log %v", err)
			} else if len(dump1) > 0 {
				filePath = filepath.Join(tempDir, fmt.Sprintf("%s_container:%s.prev.log", pod, container))
				f1, err := os.Create(filePath)
				if err != nil {
					return err
				}
				defer func() {
					if err = f1.Close(); err != nil {
						log.Warnf("Error during closing file: %v\n", err)
					}
				}()
				if _, err = f1.WriteString(fmt.Sprintf("%s\n", dump1)); err != nil {
					return err
				}
			}
		}
		return nil
	}

	_, err := Shell("kubectl get ingress --all-namespaces --kubeconfig=%s", kubeconfig)
	if err != nil {
		return err
	}
	lines, err := Shell("kubectl get pods -n %s --kubeconfig=%s", namespace, kubeconfig)
	if err != nil {
		return err
	}
	pods := strings.Split(lines, "\n")
	if len(pods) > 1 {
		for _, line := range pods[1:] {
			if idxEndOfPodName := strings.Index(line, " "); idxEndOfPodName > 0 {
				pod := line[:idxEndOfPodName]
				log.Infof("Fetching logs on %s", pod)
				if err := fetchAndWrite(pod); err != nil {
					multiErr = multierror.Append(multiErr, err)
				}
			}
		}
	}

	for _, resrc := range logDumpResources {
		log.Info(fmt.Sprintf("Fetching deployment info on %s\n", resrc))
		filePath := filepath.Join(tempDir, fmt.Sprintf("%s.yaml", resrc))
		if yaml, err0 := ShellMuteOutput(
			fmt.Sprintf("kubectl get %s -n %s -o yaml --kubeconfig=%s", resrc, namespace, kubeconfig)); err0 != nil {
			multiErr = multierror.Append(multiErr, err0)
		} else {
			if f, err1 := os.Create(filePath); err1 != nil {
				multiErr = multierror.Append(multiErr, err1)
			} else {
				if _, err2 := f.WriteString(fmt.Sprintf("%s\n", yaml)); err2 != nil {
					multiErr = multierror.Append(multiErr, err2)
				}
			}
		}
	}
	return multiErr
}

// WaitForDeploymentsReady wait up to 'timeout' duration
// return an error if deployments are not ready
func WaitForDeploymentsReady(ns string, timeout time.Duration, kubeconfig string) error {
	retry := Retrier{
		BaseDelay:   10 * time.Second,
		MaxDelay:    10 * time.Second,
		MaxDuration: timeout,
		Retries:     20,
	}

	_, err := retry.Retry(context.Background(), func(_ context.Context, _ int) error {
		nr, err := CheckDeploymentsReady(ns, kubeconfig)
		if err != nil {
			return &Break{err}
		}

		if nr == 0 { // done
			return nil
		}
		return fmt.Errorf("%d deployments not ready", nr)
	})
	return err
}

// CheckDeploymentsReady checks if deployment resources are ready.
// get podsReady() sometimes gets pods created by the "Job" resource which never reach the "Running" steady state.
func CheckDeploymentsReady(ns string, kubeconfig string) (int, error) {
	CMD := "kubectl -n %s get deployments -ao jsonpath='{range .items[*]}{@.metadata.name}{\" \"}" +
		"{@.status.availableReplicas}{\"\\n\"}{end}' --kubeconfig=%s"
	out, err := Shell(fmt.Sprintf(CMD, ns, kubeconfig))

	if err != nil {
		return 0, fmt.Errorf("could not list deployments in namespace %q: %v", ns, err)
	}

	notReady := 0
	for _, line := range strings.Split(out, "\n") {
		flds := strings.Fields(line)
		if len(flds) < 2 {
			continue
		}
		if flds[1] == "0" { // no replicas ready
			notReady++
		}
	}

	if notReady == 0 {
		log.Infof("All deployments are ready")
	}
	return notReady, nil
}

// GetKubeConfig will create a kubeconfig file based on the active environment the test is run in
func GetKubeConfig(filename string) error {
	_, err := ShellMuteOutput("kubectl config view --raw=true --minify=true > %s", filename)
	if err != nil {
		return err
	}
	log.Infof("kubeconfig file %s created\n", filename)
	return nil
}

// CheckPodRunning return if a given pod with labeled name in a namespace are in "Running" status
func CheckPodRunning(n, name string, kubeconfig string) error {
	retry := Retrier{
		BaseDelay: 30 * time.Second,
		MaxDelay:  30 * time.Second,
		Retries:   6,
	}

	retryFn := func(_ context.Context, i int) error {
		pod, err := GetPodName(n, name, kubeconfig)
		if err != nil {
			return err
		}
		ready := true
		if status := GetPodStatus(n, pod, kubeconfig); status != "Running" {
			log.Infof("%s in namespace %s is not running: %s", pod, n, status)
			ready = false
		}

		if !ready {
			return fmt.Errorf("pod %s is not ready", pod)
		}
		return nil
	}
	ctx := context.Background()
	_, err := retry.Retry(ctx, retryFn)
	if err != nil {
		return err
	}
	log.Infof("Got the pod name=%s running!", name)
	return nil
}

// CreateMultiClusterSecrets will create the secrets and configmap associated with the remote cluster
func CreateMultiClusterSecrets(namespace string, KubeClient kubernetes.Interface, RemoteKubeConfig string, localKubeConfig string) error {
	const (
		secretName    = "remote-cluster"
		configMapName = "clusterregistry"
	)
	_, err := ShellMuteOutput("kubectl create secret generic %s --from-file %s -n %s --kubeconfig=%s", secretName, RemoteKubeConfig, namespace, localKubeConfig)
	// The cluster name is derived from the filename used to create the secret we will need it for the configmap
	filename := filepath.Base(RemoteKubeConfig)
	if err != nil {
		return err
	}
	log.Infof("Secret remote-cluster created\n")
	remoteCluster := &v1.ConfigMap{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
		},
	}

	remoteClusterData := v1alpha1.Cluster{
		TypeMeta: meta_v1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "clusterregistry.k8s.io/v1alpha1",
		},
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Annotations: map[string]string{"config.istio.io/accessConfigSecret": secretName,
				"config.istio.io/accessConfigSecretNamespace": namespace,
				"config.istio.io/platform":                    "Kubernetes"},
			ClusterName: "",
		},
		Spec: v1alpha1.ClusterSpec{
			KubernetesAPIEndpoints: v1alpha1.KubernetesAPIEndpoints{
				ServerEndpoints: nil,
				CABundle:        nil,
			},
			AuthInfo: v1alpha1.AuthInfo{},
		},
	}

	dataBytes, err1 := yaml.Marshal(remoteClusterData)
	if err1 != nil {
		return err1
	}

	data := map[string]string{}
	data[filename] = string(dataBytes)
	remoteCluster.Data = data

	_, err = KubeClient.CoreV1().ConfigMaps(namespace).Create(remoteCluster)
	if err != nil {
		return err
	}
	log.Infof("Configmap created\n")
	return nil
}
