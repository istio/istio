// Copyright 2020 Istio Authors
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

package e2etaint

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"

	"istio.io/istio/pkg/test/kube"

	v12 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	client "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/cni/pkg/taint"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	TimeDuration = 60 * time.Second
	TimeDelay    = 10 * time.Millisecond
)

var (
	Test1NS = v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test1",
		},
	}
	Test2NS = v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test2",
		},
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
func createDaemonset(client *client.Interface, fileLocation string) (daemonset *v12.DaemonSet, err error) {
	obj, err := buildDaemonSetFromYAML(fileLocation)
	if err != nil {
		log.Fatalf(err.Error())
	}
	daemonset, err = (*client).AppsV1().DaemonSets(obj.Namespace).Create(context.TODO(), &obj, metav1.CreateOptions{})
	if err != nil {
		log.Fatalf(err.Error())
		return nil, err
	}
	return daemonset, err
}

//build configmap given name and namespace in cmd.ControllerOptions and location
func buildConfigIfNotExists(clientSet client.Interface, options taint.Options, fileLocation string) (configmap *v1.ConfigMap, err error) {
	err = clientSet.CoreV1().ConfigMaps(options.ConfigmapNamespace).Delete(context.TODO(), options.ConfigmapName, metav1.DeleteOptions{})
	fmt.Println(filepath.Abs("./"))
	currpath, err := filepath.Abs("./")
	cmd := exec.Command("kubectl", "create", "configmap",
		options.ConfigmapName, "-n", options.ConfigmapNamespace,
		"--from-file="+fmt.Sprintf("%s/%s/", currpath, fileLocation))
	fmt.Println(cmd.String())
	err = cmd.Run()
	err = retry.UntilSuccess(func() error {
		configmap, err = clientSet.CoreV1().ConfigMaps(options.ConfigmapNamespace).Get(context.TODO(), options.ConfigmapName, metav1.GetOptions{})
		return err
	}, retry.Converge(1), retry.Timeout(TimeDuration), retry.Delay(TimeDelay))
	if err != nil {
		log.Fatalf(err.Error())
	}
	return configmap, err
}
func waitForConfigMapDeletion(client client.Interface, name, namespace string) error {
	err := retry.UntilSuccess(func() error {
		_, err := client.CoreV1().ConfigMaps(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err == nil {
			return fmt.Errorf("configmap %s should be deleted", name)
		}
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}, retry.Converge(1), retry.Timeout(TimeDuration), retry.Delay(TimeDelay))
	return err
}
func waitForDaemonSetDeletion(client client.Interface, name, namespace string) error {
	err := retry.UntilSuccess(func() error {
		_, err := client.AppsV1().DaemonSets(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err == nil {
			return fmt.Errorf("configmap %s should be deleted", name)
		}
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}, retry.Converge(1), retry.Timeout(TimeDuration), retry.Delay(TimeDelay))
	return err
}
func TestTaintController(t *testing.T) {
	type configLocation struct {
		configmapName      string
		configmapNamespace string
		fileLocation       string
	}
	type args struct {
		config             configLocation
		daemonsetLocations []string
		namespaces         []v1.Namespace
	}
	tests := []struct {
		name string
		args args
	}{{
		name: "single case",
		args: args{
			config:             configLocation{fileLocation: "testdata/singleConfig", configmapNamespace: "default", configmapName: "single"},
			daemonsetLocations: []string{"testdata/podsConfig/single-critical-label.yaml"},
			namespaces:         []v1.Namespace{Test1NS},
		},
	},
		{
			name: "multiple labels in two namespace",
			args: args{
				config:             configLocation{fileLocation: "testdata/multiConfig", configmapNamespace: "default", configmapName: "multi"},
				daemonsetLocations: []string{"testdata/podsConfig/single-critical-label.yaml", "testdata/podsConfig/addon-test2-in-test2.yaml"},
				namespaces:         []v1.Namespace{Test1NS, Test2NS},
			},
		},
		{
			name: "multiple labels in one namespace",
			args: args{
				config:             configLocation{fileLocation: "testdata/multiInOneConfig", configmapNamespace: "default", configmapName: "mutli.one"},
				daemonsetLocations: []string{"testdata/podsConfig/single-critical-label.yaml", "testdata/podsConfig/addon-test1.yaml"},
				namespaces:         []v1.Namespace{Test1NS},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientSet, err := clientSetup()
			if err != nil {
				t.Fatalf(err.Error())
			}
			for _, ns := range tt.args.namespaces {
				_, err = clientSet.CoreV1().Namespaces().Create(context.TODO(), &ns, metav1.CreateOptions{})
				if err != nil {
					t.Fatalf(err.Error())
				}
				err = retry.UntilSuccess(func() error {
					if kube.NamespaceExists(clientSet, ns.Name) {
						return nil
					}
					return fmt.Errorf("cannot create ns %s", ns.Name)
				}, retry.Timeout(TimeDuration), retry.Delay(TimeDelay))
				if err != nil {
					t.Fatalf(err.Error())
				}
			}
			TaintOptions := &taint.Options{
				ConfigmapName:      tt.args.config.configmapName,
				ConfigmapNamespace: tt.args.config.configmapNamespace,
			}
			_, err = buildConfigIfNotExists(clientSet, *TaintOptions, tt.args.config.fileLocation)
			if err != nil {
				t.Fatalf(err.Error())
			}
			taintSetter, err := taint.NewTaintSetter(clientSet, TaintOptions)
			if err != nil {
				log.Fatalf("Could not construct taint setter: %s", err)
			}
			tc, err := taint.NewTaintSetterController(taintSetter)
			if err != nil {
				log.Fatalf("Could not build controller %s", err)
			}
			stopCh := make(chan struct{})
			go tc.Run(stopCh)
			tc.RegistTaints()
			//here all nodes have readiness taint
			err = retry.UntilSuccess(func() error {
				for _, node := range tc.ListAllNode() {
					if !taintSetter.HasReadinessTaint(node) {
						return fmt.Errorf("cannot taint %s", node.Name)
					}
				}
				return nil
			}, retry.Timeout(TimeDuration), retry.Delay(TimeDelay))
			if err != nil {
				t.Fatalf(err.Error())
			}
			daemonsets := make([]*v12.DaemonSet, 0)
			for _, location := range tt.args.daemonsetLocations {
				daemonset, err := createDaemonset(&taintSetter.Client, location)
				if err != nil {
					t.Fatalf(err.Error())
				}
				daemonsets = append(daemonsets, daemonset)
			}
			err = retry.UntilSuccess(func() error {
				for _, node := range tc.ListAllNode() {
					if taintSetter.HasReadinessTaint(node) {
						return fmt.Errorf("node %s have readiness taint", node.Name)
					}
				}
				return nil
			}, retry.Timeout(TimeDuration), retry.Delay(TimeDelay))
			if err != nil {
				t.Fatalf(err.Error())
			}
			close(stopCh)
			_ = clientSet.CoreV1().ConfigMaps(TaintOptions.ConfigmapNamespace).Delete(context.TODO(), TaintOptions.ConfigmapName, metav1.DeleteOptions{})
			err = waitForConfigMapDeletion(taintSetter.Client, TaintOptions.ConfigmapName, TaintOptions.ConfigmapNamespace)
			if err != nil {
				t.Fatalf(err.Error())
			}
			for _, daemonset := range daemonsets {
				_ = clientSet.AppsV1().DaemonSets(daemonset.Namespace).Delete(context.TODO(), daemonset.Name, metav1.DeleteOptions{})
				err = waitForDaemonSetDeletion(taintSetter.Client, daemonset.Name, daemonset.Namespace)
				if err != nil {
					t.Fatalf(err.Error())
				}
			}
			for _, ns := range tt.args.namespaces {
				_ = clientSet.CoreV1().Namespaces().Delete(context.TODO(), ns.ObjectMeta.GetName(), metav1.DeleteOptions{})
				err = kube.WaitForNamespaceDeletion(taintSetter.Client, ns.Name, retry.Timeout(TimeDuration), retry.Delay(TimeDelay))
				if err != nil {
					t.Fatalf(err.Error())
				}
			}
		})
	}
}
