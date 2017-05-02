// Copyright 2017 Istio Inc.
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
	if _, err := Shell(fmt.Sprintf("kubectl create namespace %s", n)); err != nil {
		return err
	}
	glog.Infof("namespace %s created\n", n)
	return nil
}

// DeleteNamespace delete a kubernetes namespace
func DeleteNamespace(n string) error {
	_, err := Shell(fmt.Sprintf("kubectl delete namespace %s", n))
	return err
}

// KubeApply kubectl apply
func KubeApply(n, yaml string) error {
	_, err := Shell(fmt.Sprintf("kubectl apply -n %s -f %s", n, yaml))
	return err
}

// GetIngress get istio ingress ip
func GetIngress(n string) (string, error) {
	standby := 0
	r := regexp.MustCompile(`^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$`)
	for i := 1; i <= 10; i++ {
		time.Sleep(time.Duration(standby) * time.Second)
		out, err := Shell(fmt.Sprintf("kubectl get svc istio-ingress-controller -n %s -o jsonpath='{.status.loadBalancer.ingress[*].ip}'", n))
		if err == nil {
			out = strings.Trim(out, "'")
			if match := r.FindString(out); match != "" {
				glog.Infof("Istio ingress: %s\n", out)
				return out, nil
			}
		} else {
			glog.Warningf("Failed to get ingress: %s", err)
		}
		glog.Infof("Tried %d times to get ingress...", i)
		standby += 5
	}
	err := fmt.Errorf("cannot get ingress")
	return "", err
}
