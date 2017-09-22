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
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/golang/glog"
)

// Fill complete a template with given values and generate a new output file
func Fill(outFile, inFile string, values interface{}) error {
	var bytes bytes.Buffer
	w := bufio.NewWriter(&bytes)
	tmpl, err := template.ParseFiles(inFile)
	if err != nil {
		return err
	}

	if err := tmpl.Execute(w, values); err != nil {
		return err
	}

	if err := w.Flush(); err != nil {
		return err
	}

	if err := ioutil.WriteFile(outFile, bytes.Bytes(), 0644); err != nil {
		return err
	}
	glog.Infof("Created %s from template %s", outFile, inFile)
	return nil
}

// CreateNamespace create a kubernetes namespace
func CreateNamespace(n string) error {
	if _, err := Shell("kubectl create namespace %s", n); err != nil {
		return err
	}
	glog.Infof("namespace %s created\n", n)
	return nil
}

// DeleteNamespace delete a kubernetes namespace
func DeleteNamespace(n string) error {
	_, err := Shell("kubectl delete namespace %s", n)
	return err
}

// NamespaceDeleted check if a kubernete namespace is deleted
func NamespaceDeleted(n string) (bool, error) {
	output, err := Shell("kubectl get namespace %s", n)
	if strings.Contains(output, "NotFound") {
		glog.V(2).Infof("namespace %s deleted\n", n)
		return true, nil
	}
	glog.V(2).Infof("namespace %s not deleted yet\n", n)
	return false, err
}

// KubeApplyContents kubectl apply from contents
func KubeApplyContents(namespace, yamlContents string) error {
	tmpfile, err := WriteTempfile(os.TempDir(), "kubeapply", ".yaml", yamlContents)
	if err != nil {
		return err
	}
	defer removeFile(tmpfile)
	return KubeApply(namespace, tmpfile)
}

// KubeApply kubectl apply from file
func KubeApply(namespace, yamlFileName string) error {
	_, err := Shell("kubectl apply -n %s -f %s", namespace, yamlFileName)
	return err
}

// KubeDeleteContents kubectl apply from contents
func KubeDeleteContents(namespace, yamlContents string) error {
	tmpfile, err := WriteTempfile(os.TempDir(), "kubedelete", ".yaml", yamlContents)
	if err != nil {
		return err
	}
	defer removeFile(tmpfile)
	return KubeDelete(namespace, tmpfile)
}

func removeFile(path string) {
	err := os.Remove(path)
	if err != nil {
		glog.Errorf("Unable to remove %s: %v", path, err)
	}
}

// KubeDelete kubectl delete from file
func KubeDelete(namespace, yamlFileName string) error {
	_, err := Shell("kubectl delete -n %s -f %s", namespace, yamlFileName)
	return err
}

// GetIngress get istio ingress ip
func GetIngress(n string) (string, error) {
	retry := Retrier{
		BaseDelay: 5 * time.Second,
		MaxDelay:  20 * time.Second,
		Retries:   20,
	}
	ri := regexp.MustCompile(`^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$`)
	//rp := regexp.MustCompile(`^[0-9]{1,5}$`) # Uncomment for minikube
	var ingress string
	retryFn := func(i int) error {
		ip, err := Shell("kubectl get svc istio-ingress -n %s -o jsonpath='{.status.loadBalancer.ingress[*].ip}'", n)
		// For minikube, comment out the previous line and uncomment the following line
		//ip, err := Shell("kubectl get po -l istio=ingress -n %s -o jsonpath='{.items[0].status.hostIP}'", n)
		if err != nil {
			return err
		}
		ip = strings.Trim(ip, "'")
		if ri.FindString(ip) == "" {
			err = fmt.Errorf("unable to find ingress ip")
			glog.Warning(err)
			return err
		}
		ingress = ip
		// For minikube, comment out the previous line and uncomment the following lines
		//port, e := Shell("kubectl get svc istio-ingress -n %s -o jsonpath='{.spec.ports[0].nodePort}'", n)
		//if e != nil {
		//	return e
		//}
		//port = strings.Trim(port, "'")
		//if rp.FindString(port) == "" {
		//	err = fmt.Errorf("unable to find ingress port")
		//	glog.Warning(err)
		//	return err
		//}
		//ingress = ip + ":" + port
		glog.Infof("Istio ingress: %s\n", ingress)
		return nil
	}
	_, err := retry.Retry(retryFn)
	return ingress, err
}

// GetIngressPod get istio ingress ip
func GetIngressPod(n string) (string, error) {
	retry := Retrier{
		BaseDelay: 5 * time.Second,
		MaxDelay:  5 * time.Minute,
		Retries:   20,
	}
	ipRegex := regexp.MustCompile(`^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$`)
	portRegex := regexp.MustCompile(`^[0-9]+$`)
	var ingress string
	retryFn := func(i int) error {
		podIP, err := Shell("kubectl get pod -l istio=ingress "+
			"-n %s -o jsonpath='{.items[0].status.hostIP}'", n)
		if err != nil {
			return err
		}
		podPort, err := Shell("kubectl get svc istio-ingress "+
			"-n %s -o jsonpath='{.spec.ports[0].nodePort}'", n)
		if err != nil {
			return err
		}
		podIP = strings.Trim(podIP, "'")
		podPort = strings.Trim(podPort, "'")
		if ipRegex.FindString(podIP) == "" {
			err = errors.New("unable to find ingress pod ip")
			glog.Warning(err)
			return err
		}
		if portRegex.FindString(podPort) == "" {
			err = errors.New("unable to find ingress pod port")
			glog.Warning(err)
			return err
		}
		ingress = fmt.Sprintf("%s:%s", podIP, podPort)
		glog.Infof("Istio ingress: %s\n", ingress)
		return nil
	}
	_, err := retry.Retry(retryFn)
	return ingress, err
}

// GetPodsName gets names of all pods in specific namespace and return in a slice
func GetPodsName(n string) (pods []string) {
	res, err := Shell("kubectl -n %s get pods -o jsonpath='{.items[*].metadata.name}'", n)
	if err != nil {
		glog.Infof("Failed to get pods name in namespace %s: %s", n, err)
		return
	}
	res = strings.Trim(res, "'")
	pods = strings.Split(res, " ")
	glog.Infof("Existing pods: %v", pods)
	return
}

// GetPodStatus gets status of a pod from a namespace
func GetPodStatus(n, pod string) string {
	status, err := Shell("kubectl -n %s get pods %s -o jsonpath='{.status.phase}", n, pod)
	if err != nil {
		glog.Infof("Failed to get status of pod %s in namespace %s: %s", pod, n, err)
		status = "Failed to get"
	}
	return strings.Trim(status, "'")
}

// CheckPodsRunning return if all pods in a namespace are in "Running" status
func CheckPodsRunning(n string) (ready bool) {
	retry := Retrier{
		BaseDelay: 30 * time.Second,
		MaxDelay:  30 * time.Second,
		Retries:   6,
	}

	retryFn := func(i int) error {
		pods := GetPodsName(n)
		ready = true
		for _, p := range pods {
			if status := GetPodStatus(n, p); status != "Running" {
				glog.Infof("%s in namespace %s is not running: %s", p, n, status)
				ready = false
			}
		}
		if !ready {
			_, err := Shell("kubectl -n %s get pods -o wide", n)
			if err != nil {
				glog.Infof("Cannot get pods: %s", err)
			}
			return fmt.Errorf("some pods are not ready")
		}
		return nil
	}
	_, err := retry.Retry(retryFn)
	if err != nil {
		return false
	}
	glog.Info("Get all pods running!")
	return true
}
