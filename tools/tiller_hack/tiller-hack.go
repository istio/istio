// Copyright 2018 Istio Authors
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

// Package main provides a tool tiller-hack, this tools allows exclude further
// tracking of CRDs objects by helm's tiller after they were deployed by means of
// helm's charts.
//
// ./tiller-hack --kubeconfig={location of kube config file} \
//               --configmap={the name of tiller's config map for istio deployment}
//               --namespace={the namespace of configmap and tiller,
//                            by default it is kube-system}
//               --restart-tiller={true/false, false is default, true will
//                                 force the tool to restart tiller}
//
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/golang/glog"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"

	"k8s.io/apimachinery/pkg/labels"

	"github.com/golang/protobuf/proto"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/proto/hapi/release"
)

var (
	kubeconfig = flag.String("kubeconfig", "",
		"Absolute path to the kubeconfig file, must be specified.")
	configMapName = flag.String("configmap", "",
		"Name of the configmap to patch, must be specified.")
	configMapNamespace = flag.String("namespace", "kube-system",
		"Namespace of the configmap, defaults to \"kube-system\"")
	restartTillerFlag = flag.Bool("restart-tiller", false,
		"Restart tiller after patching the configmap")

	// In older versions of helm, the compression of release information has not been used.
	// This header identifies whether the release information stored in the configmap
	// is compressed or not.
	magicGzip = []byte{0x1f, 0x8b, 0x08}
)

// BuildClient returns kubernetes clientset
func BuildClient(kubeconfig string) (*kubernetes.Clientset, error) {
	var config *rest.Config
	var err error

	kubeconfigEnv := os.Getenv("KUBECONFIG")

	if kubeconfigEnv != "" {
		kubeconfig = kubeconfigEnv
	}

	if kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, err
	}
	k8s, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return k8s, nil
}

func getConfigmapData(k8sClient *kubernetes.Clientset, configMapName, configMapNamespace string) ([]byte, error) {
	cfgMap, err := k8sClient.Core().ConfigMaps(configMapNamespace).Get(configMapName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("fail to get configmap %s/%s with error: %+v", configMapNamespace, configMapName, err)
	}
	data, ok := cfgMap.Data["release"]
	if !ok {
		return nil, fmt.Errorf("fail to locate \"release\" key in configmap %s/%s", configMapNamespace, configMapName)
	}
	return []byte(data), nil
}

func makeConfigMapBackup(k8sClient *kubernetes.Clientset, configMapName, configMapNamespace string) error {
	cfgMap, err := k8sClient.Core().ConfigMaps(configMapNamespace).Get(configMapName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("fail to get configmap %s/%s with error: %+v", configMapNamespace, configMapName, err)
	}
	bckpCfgMap := v1.ConfigMap{}
	cfgMap.DeepCopyInto(&bckpCfgMap)
	bckpCfgMap.ResourceVersion = ""

	timestamp := strings.Replace(time.Now().Format("2006-01-0215:04:05.999"), "-", "", -1)
	timestamp = strings.Replace(timestamp, ":", "", -1)
	bckpCfgMap.ObjectMeta.Name += "." + timestamp + ".backup"
	_, err = k8sClient.Core().ConfigMaps(configMapNamespace).Create(&bckpCfgMap)

	return err
}

func decodeRelease(data []byte) (*release.Release, error) {
	// base64 decode string
	b, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return nil, err
	}

	if bytes.Equal(b[0:3], magicGzip) {
		r, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		b2, err := ioutil.ReadAll(r)
		if err != nil {
			return nil, err
		}
		b = b2
	}

	var rls release.Release
	// unmarshal protobuf bytes
	if err := proto.Unmarshal(b, &rls); err != nil {
		return nil, err
	}
	return &rls, nil
}

func encodeRelease(rls *release.Release) (string, error) {
	b, err := proto.Marshal(rls)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return "", err
	}
	if _, err = w.Write(b); err != nil {
		return "", err
	}
	w.Close()

	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func updateConfigMap(k8sClient *kubernetes.Clientset, configMapName, configMapNamespace, updatedData string) error {
	cfgMap, err := k8sClient.Core().ConfigMaps(configMapNamespace).Get(configMapName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("fail to get configmap %s/%s with error: %+v", configMapNamespace, configMapName, err)
	}
	cfgMap.Data["release"] = updatedData

	err = retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		_, err = k8sClient.CoreV1().ConfigMaps(configMapNamespace).Update(cfgMap)
		return
	})
	return err
}

func restartTiller(k8sClient *kubernetes.Clientset, namespace string) error {
	selector := labels.SelectorFromSet(labels.Set(map[string]string{"app": "helm", "name": "tiller"}))
	options := metav1.ListOptions{LabelSelector: selector.String()}
	pods, err := k8sClient.Core().Pods(namespace).List(options)
	if err != nil {
		return err
	}
	if len(pods.Items) != 1 {
		return fmt.Errorf("got unexpected number of tiller pod, failing")
	}
	var gracePeriod int64
	return k8sClient.Core().Pods(namespace).Delete(pods.Items[0].ObjectMeta.Name,
		&metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod})
}

func main() {
	flag.Parse()
	flag.Set("logtostderr", "true")
	var err error
	k8sClient, err := BuildClient(*kubeconfig)
	if err != nil {
		glog.Errorf("Failed to create a client: %+v\n", err)
		os.Exit(1)
	}
	if *configMapName == "" {
		glog.Errorf("configmap name is missing.")
		os.Exit(1)
	}
	if err = makeConfigMapBackup(k8sClient, *configMapName, *configMapNamespace); err != nil {
		glog.Errorf("failed to backup original Configmap with error: %+v", err)
		os.Exit(1)
	}
	data, err := getConfigmapData(k8sClient, *configMapName, *configMapNamespace)
	if err != nil {
		glog.Errorf("failed to get Configmap's Data with error: %+v", err)
		os.Exit(1)
	}
	release, err := decodeRelease(data)
	if err != nil {
		glog.Errorf("Fail to decode Release Data of configmap with error: %+v", err)
		os.Exit(1)
	}
	var updatedTemplate []*chart.Template
	for _, template := range release.Chart.Templates {
		if template.Name == "templates/crds.yaml" || template.Name == "templates/install-custom-resources.sh.tpl" {
			continue
		}
		updatedTemplate = append(updatedTemplate, template)
	}
	release.Chart.Templates = updatedTemplate
	updatedData, err := encodeRelease(release)
	if err != nil {
		glog.Errorf("Fail to encode Release Data of configmap with error: %+v", err)
		os.Exit(1)
	}
	if err = updateConfigMap(k8sClient, *configMapName, *configMapNamespace, updatedData); err != nil {
		glog.Errorf("Fail to update configmap with error: %+v", err)
		os.Exit(1)
	}
	glog.Infof("All done...")
	if *restartTillerFlag {
		if err := restartTiller(k8sClient, *configMapNamespace); err != nil {
			glog.Warningf("tiller failed to be restarted with error: %+v", err)
			glog.Warningf("and should be restarted manually")
		}
	} else {
		glog.Infof("tiller should be restarted to refresh its data store...")
	}
}
