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

package framework

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/util"
)

func getKubeConfigFromFile(dirname string) (string, error) {
	// The tests assume that only a single remote cluster (i.e. a single file) is in play

	var remoteKube string
	err := filepath.Walk(dirname, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if (info.Mode() & os.ModeType) != 0 {
			return nil
		}
		_, err = ioutil.ReadFile(path)
		if err != nil {
			log.Warnf("Failed to read %s: %v", path, err)
			return err
		}
		remoteKube = path
		return nil
	})
	if err != nil {
		return "", nil
	}
	return remoteKube, nil
}

func (k *KubeInfo) getEndpointIPForService(svc string) (ip string, err error) {
	getOpt := meta_v1.GetOptions{IncludeUninitialized: true}
	var eps *v1.Endpoints
	// Wait until endpoint is obtained
	for i := 0; i <= 200; i++ {
		eps, err = k.KubeClient.CoreV1().Endpoints(k.Namespace).Get(svc, getOpt)
		if (len(eps.Subsets) == 0) || (err != nil) {
			time.Sleep(time.Second * 1)
		} else {
			break
		}
	}

	if err == nil && eps != nil {
		if len(eps.Subsets[0].Addresses) != 0 {
			ip = eps.Subsets[0].Addresses[0].IP
		} else if len(eps.Subsets[0].NotReadyAddresses) != 0 {
			ip = eps.Subsets[0].NotReadyAddresses[0].IP
		} else {
			err = fmt.Errorf("could not get endpoint addresses for service %s", svc)
			return "", err
		}
	}
	return
}

func (k *KubeInfo) generateRemoteIstio(src, dst string) error {
	content, err := ioutil.ReadFile(src)
	if err != nil {
		return fmt.Errorf("cannot read remote yaml file %s", src)
	}

	svcs := []string{"istio-pilot", "istio-policy", "istio-ingress", "istio-telemetry"}
	for _, svc := range svcs {
		var ip string
		ip, err = k.getEndpointIPForService(svc)
		if err == nil {
			replaceStr := fmt.Sprintf("%s.istio-system", svc)
			content = replacePattern(content, replaceStr, ip)
			log.Infof("Service %s has an endpoint IP %s", svc, ip)
		} else {
			return err
		}
	}
	content = replacePattern(content, istioSystem, k.Namespace)
	err = ioutil.WriteFile(dst, content, 0600)
	if err != nil {
		return fmt.Errorf("cannot write remote into generated yaml file %s", dst)
	}
	return nil
}

func (k *KubeInfo) createCacerts(remoteCluster bool) (err error) {
	kc := k.KubeConfig
	cluster := "primary"
	if remoteCluster {
		kc = k.RemoteKubeConfig
		cluster = "remote"
	}
	caCertFile := filepath.Join(k.ReleaseDir, caCertFileName)
	caKeyFile := filepath.Join(k.ReleaseDir, caKeyFileName)
	rootCertFile := filepath.Join(k.ReleaseDir, rootCertFileName)
	certChainFile := filepath.Join(k.ReleaseDir, certChainFileName)
	if _, err = util.Shell("kubectl create secret generic cacerts --kubeconfig=%s -n %s "+
		"--from-file=%s --from-file=%s --from-file=%s --from-file=%s",
		kc, k.Namespace, caCertFile, caKeyFile, rootCertFile, certChainFile); err == nil {
		log.Infof("Created Cacerts with namespace %s in %s cluster", k.Namespace, cluster)
	}
	return err
}
