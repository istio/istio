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

	"github.com/davecgh/go-spew/spew"
	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/log"
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
func (k *KubeInfo) createRemoteSecrets() error {
	var err error
	var caSecret *v1.Secret
	// need to give time for the secret to be created on the local cluster
	for i := 0; i <= 200; i++ {
		caSecret, err = k.KubeClient.CoreV1().Secrets(k.Namespace).Get("istio.default", meta_v1.GetOptions{})
		if err == nil {
			break
		}
		time.Sleep(time.Second * 1)
	}
	if err != nil {
		err = fmt.Errorf("could not get secret from local cluster")
		return err
	}

	caSecret.ObjectMeta.ResourceVersion = ""
	_, err = k.RemoteKubeClient.CoreV1().Secrets(k.Namespace).Create(caSecret)
	if err != nil {
		err = fmt.Errorf("could not create secret on remote cluster")
		return err
	}
	return nil
}

func (k *KubeInfo) generateRemoteIstio(src, dst string) error {
	content, err := ioutil.ReadFile(src)
	if err != nil {
		log.Errorf("cannot read remote yaml file %s", src)
		return err
	}
	getOpt := meta_v1.GetOptions{IncludeUninitialized: true}
	var pilotEPS, policyEPS, statsdEPS *v1.Endpoints

	for i := 0; i <= 200; i++ {
		pilotEPS, err = k.KubeClient.CoreV1().Endpoints(k.Namespace).Get("istio-pilot", getOpt)
		log.Infof("PilotEPS = %s ", pilotEPS)
		if (len(pilotEPS.Subsets) != 0) && (err == nil) {

			policyEPS, err = k.KubeClient.CoreV1().Endpoints(k.Namespace).Get("istio-policy", getOpt)
			log.Infof("policyEPS = %s ", policyEPS)
			if (len(policyEPS.Subsets) != 0) && (err == nil) {
				break
			}
			statsdEPS, err = k.KubeClient.CoreV1().Endpoints(k.Namespace).Get("istio-statsd-prom-bridge", getOpt)
			log.Infof("statsdEPS = %s ", policyEPS)
			if (len(statsdEPS.Subsets) != 0) && (err == nil) {
				break
			}
		}
		time.Sleep(time.Second * 1)
	}
	log.Infof("len pilot %i len policyEPS = %i ", len(pilotEPS.Subsets), len(policyEPS.Subsets))
	log.Infof("pilotEPS %s", spew.Sdump(pilotEPS))
	log.Infof("policyEPS %s", spew.Sdump(policyEPS))
	log.Infof("statsdEPS %s", spew.Sdump(statsdEPS))
	if err != nil {
		err = fmt.Errorf("could not get endpoints from local cluster")
		return err
	}

	var pilotIP, policyIP, statsdIP string
	if len(pilotEPS.Subsets[0].Addresses) != 0 {
		pilotIP = pilotEPS.Subsets[0].Addresses[0].IP
	} else if len(pilotEPS.Subsets[0].NotReadyAddresses) != 0 {
		pilotIP = pilotEPS.Subsets[0].NotReadyAddresses[0].IP
	} else {
		err = fmt.Errorf("could not get endpoint addresses")
		return err
	}
	if len(policyEPS.Subsets[0].Addresses) != 0 {
		policyIP = policyEPS.Subsets[0].Addresses[0].IP
	} else if len(pilotEPS.Subsets[0].NotReadyAddresses) != 0 {
		policyIP = policyEPS.Subsets[0].NotReadyAddresses[0].IP
	} else {
		err = fmt.Errorf("could not get endpoint addresses")
		return err
	}
	if len(statsdEPS.Subsets[0].Addresses) != 0 {
		statsdIP = statsdEPS.Subsets[0].Addresses[0].IP
	} else if len(statsdEPS.Subsets[0].NotReadyAddresses) != 0 {
		statsdIP = statsdEPS.Subsets[0].NotReadyAddresses[0].IP
	} else {
		err = fmt.Errorf("could not get endpoint addresses")
		return err
	}
	log.Infof("istio-pilot endpoint IP = %s istio-policy endpoint IP = %s statsd endpoint IP %s", pilotIP, policyIP, statsdIP)
	content = replacePattern(content, istioSystem, k.Namespace)
	content = replacePattern(content, "istio-policy.istio-system", policyIP)
	content = replacePattern(content, "istio-pilot.istio-system", pilotIP)
	content = replacePattern(content, "istio-statsd.istio-system", statsdIP)
	err = ioutil.WriteFile(dst, content, 0600)
	if err != nil {
		log.Errorf("cannot write remote into generated yaml file %s", dst)
	}
	return nil
}
