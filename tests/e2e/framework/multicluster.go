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

func (k *KubeInfo) generateRemoteIstio(src, dst string) error {
	content, err := ioutil.ReadFile(src)
	if err != nil {
		return fmt.Errorf("cannot read remote yaml file %s", src)
	}
	getOpt := meta_v1.GetOptions{IncludeUninitialized: true}
	var statsdEPS, pilotEPS, mixerEPS *v1.Endpoints
	var i int

	for i = 0; i <= 200; i++ {
		pilotEPS, err = k.KubeClient.CoreV1().Endpoints(k.Namespace).Get("istio-pilot", getOpt)
		if (len(pilotEPS.Subsets) != 0) && (err == nil) {
			mixerEPS, err = k.KubeClient.CoreV1().Endpoints(k.Namespace).Get("istio-policy", getOpt)
			if (len(mixerEPS.Subsets) != 0) && (err == nil) {
				statsdEPS, err = k.KubeClient.CoreV1().Endpoints(k.Namespace).Get("istio-statsd-prom-bridge", getOpt)
				if (len(statsdEPS.Subsets) != 0) && (err == nil) {
					break
				}
			}
		}
		time.Sleep(time.Second * 1)
	}
	if (err != nil) || (i >= 200) {
		return fmt.Errorf("could not get endpoints from local cluster")
	}

	var statsdIP, pilotIP, mixerIP string
	if len(pilotEPS.Subsets[0].Addresses) != 0 {
		pilotIP = pilotEPS.Subsets[0].Addresses[0].IP
	} else if len(pilotEPS.Subsets[0].NotReadyAddresses) != 0 {
		pilotIP = pilotEPS.Subsets[0].NotReadyAddresses[0].IP
	} else {
		return fmt.Errorf("could not get endpoint addresses")
	}
	if len(mixerEPS.Subsets[0].Addresses) != 0 {
		mixerIP = mixerEPS.Subsets[0].Addresses[0].IP
	} else if len(mixerEPS.Subsets[0].NotReadyAddresses) != 0 {
		mixerIP = mixerEPS.Subsets[0].NotReadyAddresses[0].IP
	} else {
		return fmt.Errorf("could not get endpoint addresses")
	}
	if len(statsdEPS.Subsets[0].Addresses) != 0 {
		statsdIP = statsdEPS.Subsets[0].Addresses[0].IP
	} else if len(statsdEPS.Subsets[0].NotReadyAddresses) != 0 {
		statsdIP = statsdEPS.Subsets[0].NotReadyAddresses[0].IP
	} else {
		return fmt.Errorf("could not get endpoint addresses")
	}
	log.Infof("istio-pilot IP = %s istio-policy IP = %s istio-statsd-prom-bridge IP = %s", pilotIP, mixerIP, statsdIP)
	content = replacePattern(content, "istio-policy.istio-system", mixerIP)
	content = replacePattern(content, "istio-pilot.istio-system", pilotIP)
	content = replacePattern(content, "istio-statsd-prom-bridge.istio-system", statsdIP)
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
