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

package taint

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	client "k8s.io/client-go/kubernetes"

	"istio.io/pkg/log"
)

const (
	TaintName = "NodeReadiness"
)

type ConfigSettings struct {
	Name          string `yaml:"name"`
	Namespace     string `yaml:"namespace"`
	LabelSelector string `yaml:"selector"`
}

func (config ConfigSettings) String() string {
	return fmt.Sprintf("name is %s, namespace is %s,  selector is %s", config.Name, config.Namespace, config.LabelSelector)
}

type Options struct {
	ConfigmapName      string
	ConfigmapNamespace string
}

type Setter struct {
	configs []ConfigSettings // contains all configmaps information
	Client  client.Interface
	mutex   sync.RWMutex
}

func (ts *Setter) Configs() []ConfigSettings {
	return ts.configs
}

func NewTaintSetter(clientset client.Interface, options *Options) (ts *Setter, err error) {
	configmap, err := clientset.CoreV1().ConfigMaps(options.ConfigmapNamespace).Get(context.TODO(), options.ConfigmapName, metav1.GetOptions{})
	if err != nil {
		return ts, err
	}
	ts = &Setter{
		configs: []ConfigSettings{},
		Client:  clientset,
	}
	ts.LoadConfig(*configmap)
	return
}

// load corresponding configmap's critical labels and their namespace
func (ts *Setter) LoadConfig(config v1.ConfigMap) {
	log.Debugf("Loading configmap %s in %s", config.Name, config.Namespace)
	ts.configs = make([]ConfigSettings, 0) // clear previous one
	for key, value := range config.Data {
		log.Debugf("Loading %s ", key)
		var tempst []ConfigSettings
		err := yaml.Unmarshal([]byte(value), &tempst)
		if err != nil {
			log.Fatalf("cannot unmarshal data : %v", err)
		}
		for _, elem := range tempst {
			for _, selector := range strings.Split(elem.LabelSelector, ",") {
				selector = strings.TrimSpace(selector)
				_, err = metav1.ParseToLabelSelector(selector)
				if err != nil {
					log.Fatalf("illegal selector, %s", err.Error())
				}
				ts.configs = append(ts.configs, ConfigSettings{Namespace: elem.Namespace, Name: elem.Name, LabelSelector: selector})
			}
		}
		log.Infof("successfully loaded %s", tempst)
	}
}

// only pod with NodeReadiness Tolerance with effect no schedule or
// a generalized tolerance with noschedule effect can be considered
func (ts *Setter) validTolerance(pod v1.Pod) bool {
	for _, toleration := range pod.Spec.Tolerations {
		if (toleration.Key == TaintName || toleration.Key == "") &&
			toleration.Operator == v1.TolerationOpExists &&
			toleration.Effect == v1.TaintEffectNoSchedule {
			return true
		}
	}
	return false
}

// check whether current node have readiness
func (ts *Setter) HasReadinessTaint(node *v1.Node) bool {
	ts.mutex.RLock()
	defer ts.mutex.RUnlock()
	for _, taint := range node.Spec.Taints {
		if taint.Key == TaintName && taint.Effect == v1.TaintEffectNoSchedule {
			return true
		}
	}

	return false
}

// assumption: order of taint is not important
func (ts *Setter) RemoveReadinessTaint(node *v1.Node) error {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	updatedTaint := deleteTaint(node.Spec.Taints, &v1.Taint{Key: TaintName, Effect: v1.TaintEffectNoSchedule})
	node.Spec.Taints = updatedTaint
	updatedNodeWithTaint, err := ts.Client.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	if err != nil || updatedNodeWithTaint == nil {
		return fmt.Errorf("failed to update node %v after adding taint: %v", node.Name, err)
	}
	log.Infof("Successfully removed taint on node %v", updatedNodeWithTaint.Name)
	return nil
}

// taint node with specific taint name with effect of no schedule
// do nothing if it already have the readiness taint
func (ts *Setter) AddReadinessTaint(node *v1.Node) error {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	for _, taint := range node.Spec.Taints {
		if taint.Key == TaintName && taint.Effect == v1.TaintEffectNoSchedule {
			log.Debugf("%v already present on node %v", TaintName, node.Name)
			return nil
		}
	}

	node.Spec.Taints = append(node.Spec.Taints, v1.Taint{
		Key:    TaintName,
		Effect: v1.TaintEffectNoSchedule,
	})
	updatedNodeWithTaint, err := ts.Client.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	if err != nil || updatedNodeWithTaint == nil {
		return fmt.Errorf("failed to update node %v after adding taint: %v", node.Name, err)
	}
	log.Infof("Successfully added taint on node %v", updatedNodeWithTaint.Name)
	return nil
}

// DeleteTaint removes all the taints that have the same key and effect to given taintToDelete.
func deleteTaint(taints []v1.Taint, taintToDelete *v1.Taint) []v1.Taint {
	newTaints := []v1.Taint{}
	for i := range taints {
		if taintToDelete.MatchTaint(&taints[i]) {
			continue
		}
		newTaints = append(newTaints, taints[i])
	}
	return newTaints
}

// node readiness validation by checking the last heartbeat status
func GetNodeLatestReadiness(node v1.Node) bool {
	currentCondition := node.Status.Conditions
	if len(currentCondition) == 0 {
		return false
	}
	sort.Slice(currentCondition, func(i, j int) bool {
		return currentCondition[i].LastHeartbeatTime.Time.Before(currentCondition[j].LastHeartbeatTime.Time)
	})
	latestCondition := currentCondition[len(currentCondition)-1]
	return latestCondition.Type == v1.NodeReady && latestCondition.Status == v1.ConditionTrue
}
