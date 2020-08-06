package taint

import (
	"context"
	"fmt"
	"sort"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	client "k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/util/podutils"

	"istio.io/pkg/log"
)

const (
	TaintName     = "NodeReadiness"
	ToleranceName = "NodeReadiness"
)

type ConfigSettings struct {
	name           string
	namespace      string
	LabelSelectors string `json:"label_selectors"`
}

func (config ConfigSettings) ToString() string {
	return fmt.Sprintf("name is %s, namespace is %s,  selector is %s", config.name, config.namespace, config.LabelSelectors)
}

type Options struct {
	ConfigName      string
	ConfigNameSpace string
}
type TaintSetter struct {
	configs []ConfigSettings // contains all configmaps information
	Client  client.Interface
}

func (ts TaintSetter) GetAllConfigs() []ConfigSettings {
	return ts.configs
}
func NewTaintSetter(clientset client.Interface, options *Options) (ts TaintSetter, err error) {
	configmap, err := clientset.CoreV1().ConfigMaps(options.ConfigNameSpace).Get(context.TODO(), options.ConfigName, metav1.GetOptions{})
	if err != nil {
		return ts, err
	}
	ts = TaintSetter{
		configs: []ConfigSettings{},
		Client:  clientset,
	}
	ts.LoadConfig(*configmap)
	return
}

// load corresponding configmap's critical labels and their namespace
func (ts *TaintSetter) LoadConfig(config v1.ConfigMap) {
	log.Debugf("Loading configmap %s in %s", config.Name, config.Namespace)
	ts.configs = make([]ConfigSettings, 0) //clear previous one
	for key, value := range config.Data {
		log.Debugf("Loading %s ", key)
		temp := strings.Split(value, "\n")
		var tempset ConfigSettings
		for _, str := range temp {
			if strings.HasPrefix(strings.TrimSpace(str), "name=") {
				pair := strings.SplitN(strings.TrimSpace(str), "=", 2)
				tempset.name = pair[1]
			} else if strings.HasPrefix(strings.TrimSpace(str), "namespace=") {
				pair := strings.SplitN(strings.TrimSpace(str), "=", 2)
				tempset.namespace = pair[1]
			} else if strings.HasPrefix(strings.TrimSpace(str), "selector=") {
				pair := strings.SplitN(strings.TrimSpace(str), "=", 2)
				tempset.LabelSelectors = pair[1]
			}
		}
		ts.configs = append(ts.configs, tempset)
		fmt.Println(ts.configs)
	}
}
func (ts TaintSetter) validTolerance(pod v1.Pod) bool {
	for _, toleration := range pod.Spec.Tolerations {
		if (toleration.Key == ToleranceName || toleration.Key == "") &&
			toleration.Operator == v1.TolerationOpExists &&
			toleration.Effect == v1.TaintEffectNoSchedule {
			return true
		}
	}
	return false
}

// find all pods with selectors and namespaces defined in config map
func (ts TaintSetter) ListCandidatePods() (list v1.PodList, err error) {
	list.Items = []v1.Pod{}
	for _, config := range ts.configs {
		var rawList *v1.PodList
		selectors := strings.Split(config.LabelSelectors, ",")
		for _, selector := range selectors {
			rawList, err = ts.Client.CoreV1().Pods(config.namespace).List(context.TODO(), metav1.ListOptions{
				LabelSelector: selector,
			})
			if err != nil {
				return
			}
			for _, pod := range rawList.Items {
				if ts.validTolerance(pod) {
					list.Items = append(list.Items, pod)
				}
			}
		}
	}
	return
}
func (ts TaintSetter) HasReadinessTaint(node *v1.Node) bool {
	taintExists := false
	for _, taint := range node.Spec.Taints {
		if taint.Key == TaintName && taint.Effect == v1.TaintEffectNoSchedule {
			taintExists = true
			break
		}
	}
	return taintExists
}

//assumption: order of taint is not important
func (ts TaintSetter) RemoveReadinessTaint(node *v1.Node) error {
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
//do nothing if it already have the readiness taint
func (ts TaintSetter) AddReadinessTaint(node *v1.Node) error {
	var taintsExists bool
	for _, taint := range node.Spec.Taints {
		if taint.Key == TaintName {
			taintsExists = true
			break
		}
	}
	if taintsExists {
		log.Debugf("%v already present on node %v", TaintName, node.Name)
		return nil
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

//get all critical pods in a node
func (ts TaintSetter) GetAllCriticalPodsInNode(node *v1.Node) (podlist v1.PodList, err error) {
	candidates, err := ts.ListCandidatePods()
	if err != nil {
		return podlist, err
	}
	podlist.Items = []v1.Pod{}
	for _, pod := range candidates.Items {
		if pod.Spec.NodeName == node.Name {
			podlist.Items = append(podlist.Items, pod)
		}
	}
	return podlist, nil
}

//return true if labels contain at least one selector in labelselector class
func containsLabel(labels map[string]string, selector string) bool {
	labelset := make(map[string]struct{})
	for key, val := range labels {
		labelset[fmt.Sprintf("%s=%s", key, val)] = struct{}{}
	}
	if _, ok := labelset[selector]; ok {
		return true
	}
	return false
}

//get pod's node and return error if it cannot find node by pod's node name
func (ts TaintSetter) GetNodeByPod(pod *v1.Pod) (*v1.Node, error) {
	updatedNode, err := ts.Client.CoreV1().Nodes().Get(context.TODO(), pod.Spec.NodeName, metav1.GetOptions{})
	if err != nil || updatedNode == nil {
		return nil, fmt.Errorf("failed to get node %v for pod %v :  %v", pod.Spec.NodeName, pod.Name, err)
	}
	return updatedNode, nil
}

// DeleteTaint removes all the the taints that have the same key and effect to given taintToDelete.
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

//check the readiness of node by looping over all critical pod inside the node
//if all labels selector in each namespace get satisfied, return true
func (ts TaintSetter) CheckNodeReadiness(node *v1.Node) bool {
	if ts.configs == nil || len(ts.configs) == 0 {
		return true
	}
	podList, err := ts.GetAllCriticalPodsInNode(node)
	if err != nil {
		log.Infof("error happened in get critical pod, %v", err)
		return false
	}
	nsToPods := ts.buildNamespaceToPodsMap(podList)
	nsToLables := ts.buildLabelReadiness()
	for ns, labels := range nsToLables {
		if _, ok := nsToPods[ns]; !ok {
			return false
		}
		currPods := nsToPods[ns]
		for _, pod := range currPods {
			if podutils.IsPodReady(&pod) {
				currLabels := ts.GetCriticalLabels(pod)
				for _, label := range currLabels {
					if _, ok := labels[label]; ok {
						nsToLables[ns][label] = true
					}
				}
			}
		}
	}
	for _, labels := range nsToLables {
		for label, readiness := range labels {
			if readiness == false {
				log.Infof("label %s in node %s not satisfied", label, node.Name)
				return false
			}
		}
	}
	return true
}

//retrieve configmap defined critical labels in a pod
func (ts TaintSetter) GetCriticalLabels(pod v1.Pod) []string {
	nsToLables := ts.buildLabelReadiness()
	if _, ok := nsToLables[pod.Namespace]; !ok {
		return []string{}
	}
	ans := make([]string, 0)
	for label := range nsToLables[pod.Namespace] {
		if containsLabel(pod.Labels, label) {
			ans = append(ans, label)
		}
	}
	return ans
}
func (ts TaintSetter) buildNamespaceToPodsMap(list v1.PodList) map[string][]v1.Pod {
	ans := make(map[string][]v1.Pod)
	for _, pod := range list.Items {
		if _, ok := ans[pod.Namespace]; !ok {
			ans[pod.Namespace] = make([]v1.Pod, 0)
		}
		ans[pod.Namespace] = append(ans[pod.Namespace], pod)
	}
	return ans
}
func (ts TaintSetter) buildLabelReadiness() map[string]map[string]bool {
	ans := make(map[string]map[string]bool)
	for _, config := range ts.configs {
		if _, ok := ans[config.namespace]; !ok {
			ans[config.namespace] = map[string]bool{}
		}
		selectors := strings.Split(config.LabelSelectors, ",")
		for _, selector := range selectors {
			ans[config.namespace][strings.TrimSpace(selector)] = false
		}
	}
	return ans
}

//retrive all nodes
func (ts TaintSetter) GetAllNodes() (*v1.NodeList, error) {
	return ts.Client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
}

// if node is ready, check all of its critical labels and if all of them are ready , remove readiness taint
//else taint it
func (taintsetter TaintSetter) ProcessNode(node *v1.Node) error {
	if GetNodeLatestReadiness(*node) {
		if taintsetter.CheckNodeReadiness(node) {
			err := taintsetter.RemoveReadinessTaint(node)
			if err != nil {
				return fmt.Errorf("Fatal error Cannot remove readiness taint in node: %s", node.Name)
			}
			log.Infof("node %+v remove readiness taint because it is ready", node.Name)
		} else {
			if taintsetter.HasReadinessTaint(node) {
				log.Infof("node %v has readiness taint because it is not ready", node.Name)
			} else {
				err := taintsetter.AddReadinessTaint(node)
				if err != nil {
					return fmt.Errorf("Fatal error Cannot add readiness taint in node: %s", node.Name)
				}
				log.Infof("node %v add readiness taint because it is not ready", node.Name)
			}
		}
	} else {
		if taintsetter.HasReadinessTaint(node) {
			log.Infof("node %v has readiness taint because it is not ready", node.Name)
		} else {
			err := taintsetter.AddReadinessTaint(node)
			if err != nil {
				return fmt.Errorf("Fatal error Cannot add readiness taint in node: %s", node.Name)
			}
			log.Infof("node %v add readiness taint because it is not ready", node.Name)
		}
	}
	return nil
}

//node readiness validation by checking the last heartbeat status
func GetNodeLatestReadiness(node v1.Node) bool {
	currentCondition := node.Status.Conditions
	if currentCondition == nil || len(currentCondition) == 0 {
		return false
	}
	sort.Slice(currentCondition, func(i, j int) bool {
		return currentCondition[i].LastHeartbeatTime.Time.Before(currentCondition[j].LastHeartbeatTime.Time)
	})
	latestCondition := currentCondition[len(currentCondition)-1]
	return latestCondition.Type == v1.NodeReady && latestCondition.Status == v1.ConditionTrue
}

//if pod is ready, check all related pod in the corresponding node, if any critical pod is unready, it should be tainted
func (taintsetter TaintSetter) ProcessReadyPod(pod *v1.Pod) error {
	node, err := taintsetter.GetNodeByPod(pod)
	if err != nil {
		return fmt.Errorf("Fatal error Cannot get node by  %s in namespace %s : %s", pod.Name, pod.Namespace, err)
	}
	if GetNodeLatestReadiness(*node) && taintsetter.CheckNodeReadiness(node) {
		err = taintsetter.RemoveReadinessTaint(node)
		if err != nil {
			return fmt.Errorf("Fatal error Cannot remove node readiness taint: %s ", err.Error())
		}
		log.Infof("Readiness Taint removed to the node %v because all pods inside is ready", node.Name)
		return nil
	} else {
		if taintsetter.HasReadinessTaint(node) {
			log.Infof("node %v has readiness taint because pod %v in namespace %v is not ready", node.Name, pod.Name, pod.Namespace)
			return nil
		} else {
			err = taintsetter.AddReadinessTaint(node)
			if err != nil {
				return fmt.Errorf("Fatal error Cannot add taint to node: %s", err.Error())
			}
			log.Infof("node %v add readiness taint because some other pods is not ready", node.Name)
			return nil
		}
	}
}

//if pod is unready, it should be tainted
func (taintsetter TaintSetter) ProcessUnReadyPod(pod *v1.Pod) error {
	node, err := taintsetter.GetNodeByPod(pod)
	if err != nil {
		return fmt.Errorf("Fatal error Cannot get node by  %s in namespace %s : %s", pod.Name, pod.Namespace, err)
	}
	if taintsetter.HasReadinessTaint(node) {
		log.Infof("node %v has readiness taint because pod %v in namespace %v is not ready", node.Name, pod.Name, pod.Namespace)
		return nil
	} else {
		err = taintsetter.AddReadinessTaint(node)
		if err != nil {
			return fmt.Errorf("Fatal error Cannot add taint to node: %s", err.Error())
		}
		log.Infof("node %+v add readiness taint because pod %v in namespace %v is not ready", node.Name, pod.Name, pod.Namespace)
		return nil
	}
}
