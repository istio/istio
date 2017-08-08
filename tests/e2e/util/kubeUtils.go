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

// KubeApply kubectl apply
func KubeApply(n, yaml string) error {
	_, err := Shell("kubectl apply -n %s -f %s", n, yaml)
	return err
}

// GetIngress get istio ingress ip
func GetIngress(n string) (string, error) {
	retry := Retrier{
		BaseDelay: 5 * time.Second,
		MaxDelay:  20 * time.Second,
		Retries:   20,
	}
	r := regexp.MustCompile(`^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$`)
	var ingress string
	retryFn := func(i int) error {
		out, err := Shell("kubectl get svc istio-ingress -n %s "+
			"-o jsonpath='{.status.loadBalancer.ingress[*].ip}'", n)
		if err != nil {
			return err
		}
		out = strings.Trim(out, "'")
		if r.FindString(out) == "" {
			err = fmt.Errorf("unable to find ingress")
			glog.Warning(err)
			return err
		}
		ingress = out
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
		MaxDelay:  20 * time.Minute,
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
