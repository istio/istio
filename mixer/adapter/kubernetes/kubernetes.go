// Copyright 2017 Google Inc.
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

// Package kubernetes provides functionality to adapt mixer behavior to the
// kubernetes environment. Primarily, it is used to generate values as part
// of the attribute generation preprocessing phase of the mixer. These values
// will be transformed into attributes that can be used for subsequent config
// resolution and adapter dispatch and execution.
package kubernetes

import (
	"fmt"
	"strings"
	"sync"
	"time"

	k8s "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/mixer/adapter/kubernetes/config"
	"istio.io/mixer/pkg/adapter"
)

type (
	builder struct {
		adapter.DefaultBuilder

		stopChan             chan struct{}
		pods                 cacheController
		once                 sync.Once
		newCacheControllerFn controllerFactoryFn
	}
	kubegen struct {
		log    adapter.Logger
		pods   cacheController
		params config.Params
	}

	// used strictly for testing purposes
	controllerFactoryFn func(kubeconfigPath string, refreshDuration time.Duration, env adapter.Env) (cacheController, error)
)

const (
	// adapter vals
	name = "kubernetes"
	desc = "Provides platform specific functionality for the kubernetes environment"

	// parsing
	kubePrefix = "kubernetes://"

	// input/output naming
	sourceUID         = "sourceUID"
	targetUID         = "targetUID"
	originUID         = "originUID"
	sourcePrefix      = "source"
	targetPrefix      = "target"
	originPrefix      = "origin"
	labelsVal         = "Labels"
	podNameVal        = "PodName"
	podIPVal          = "PodIP"
	hostIPVal         = "HostIP"
	namespaceVal      = "Namespace"
	serviceAccountVal = "ServiceAccountName"
	serviceVal        = "Service"

	// value extraction
	podServiceLabel = "app"

	// cache invaliation
	// TODO: determine a reasonable default
	defaultRefreshPeriod = 5 * time.Minute
)

var (
	conf = &config.Params{
		KubeconfigPath:          "",
		CacheRefreshDuration:    defaultRefreshPeriod,
		SourceUidInputName:      sourceUID,
		TargetUidInputName:      targetUID,
		OriginUidInputName:      originUID,
		PodLabelForService:      podServiceLabel,
		SourcePrefix:            sourcePrefix,
		TargetPrefix:            targetPrefix,
		OriginPrefix:            originPrefix,
		LabelsValueName:         labelsVal,
		PodNameValueName:        podNameVal,
		PodIpValueName:          podIPVal,
		HostIpValueName:         hostIPVal,
		NamespaceValueName:      namespaceVal,
		ServiceAccountValueName: serviceAccountVal,
		ServiceValueName:        serviceVal,
	}
)

// Register records the builders exposed by this adapter.
func Register(r adapter.Registrar) {
	r.RegisterAttributesGeneratorBuilder(newBuilder(newCacheFromConfig))
}

func newBuilder(cacheFactory controllerFactoryFn) *builder {
	stopChan := make(chan struct{})
	return &builder{adapter.NewDefaultBuilder(name, desc, conf), stopChan, nil, sync.Once{}, cacheFactory}
}

func (b *builder) Close() error {
	close(b.stopChan)
	return nil
}

func (*builder) ValidateConfig(c adapter.Config) (ce *adapter.ConfigErrors) {
	params := c.(*config.Params)
	if len(params.SourceUidInputName) == 0 {
		ce = ce.Appendf("source_uid_input_name", "field must be populated")
	}
	if len(params.TargetUidInputName) == 0 {
		ce = ce.Appendf("target_uid_input_name", "field must be populated")
	}
	if len(params.OriginUidInputName) == 0 {
		ce = ce.Appendf("origin_uid_input_name", "field must be populated")
	}
	if len(params.SourcePrefix) == 0 {
		ce = ce.Appendf("source_prefix", "field must be populated")
	}
	if len(params.TargetPrefix) == 0 {
		ce = ce.Appendf("target_prefix", "field must be populated")
	}
	if len(params.OriginPrefix) == 0 {
		ce = ce.Appendf("origin_prefix", "field must be populated")
	}
	if len(params.LabelsValueName) == 0 {
		ce = ce.Appendf("labels_value_name", "field must be populated")
	}
	if len(params.PodIpValueName) == 0 {
		ce = ce.Appendf("pod_ip_value_name", "field must be populated")
	}
	if len(params.PodNameValueName) == 0 {
		ce = ce.Appendf("pod_name_value_name", "field must be populated")
	}
	if len(params.HostIpValueName) == 0 {
		ce = ce.Appendf("host_ip_value_name", "field must be populated")
	}
	if len(params.NamespaceValueName) == 0 {
		ce = ce.Appendf("namespace_value_name", "field must be populated")
	}
	if len(params.ServiceAccountValueName) == 0 {
		ce = ce.Appendf("service_account_value_name", "field must be populated")
	}
	if len(params.ServiceValueName) == 0 {
		ce = ce.Appendf("service_value_name", "field must be populated")
	}
	if len(params.PodLabelForService) == 0 {
		ce = ce.Appendf("pod_label_name", "field must be populated")
	}
	return
}

func (b *builder) BuildAttributesGenerator(env adapter.Env, c adapter.Config) (adapter.AttributesGenerator, error) {
	var clientErr error
	paramsProto := c.(*config.Params)
	b.once.Do(func() {
		refresh := paramsProto.CacheRefreshDuration
		controller, err := b.newCacheControllerFn(paramsProto.KubeconfigPath, refresh, env)
		if err != nil {
			clientErr = err
			return
		}
		b.pods = controller
		env.ScheduleDaemon(func() { b.pods.Run(b.stopChan) })
		// ensure that any request is only handled after
		// a sync has occurred
		cache.WaitForCacheSync(b.stopChan, b.pods.HasSynced)
	})
	if clientErr != nil {
		return nil, clientErr
	}
	kg := &kubegen{
		log:    env.Logger(),
		pods:   b.pods,
		params: *paramsProto,
	}
	return kg, nil
}

func newCacheFromConfig(kubeconfigPath string, refreshDuration time.Duration, env adapter.Env) (cacheController, error) {
	env.Logger().Infof("getting kubeconfig from: %#v", kubeconfigPath)
	config, err := getRESTConfig(kubeconfigPath)
	if err != nil || config == nil {
		return nil, fmt.Errorf("could not retrieve kubeconfig: %v", err)
	}
	env.Logger().Infof("getting k8s client with %#v", config)
	clientset, err := k8s.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("could not create clientset for k8s: %v", err)
	}
	env.Logger().Infof("building new cache controller")
	return newCacheController(clientset, "", refreshDuration, env), nil
}

func getRESTConfig(kubeconfigPath string) (*rest.Config, error) {
	return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
}

func (k *kubegen) Close() error { return nil }

func (k *kubegen) Generate(inputs map[string]interface{}) (map[string]interface{}, error) {
	values := make(map[string]interface{})
	if uid, found := inputs[k.params.SourceUidInputName]; found {
		uidstr := uid.(string)
		k.addValues(values, uidstr, k.params.SourcePrefix)
	}
	if uid, found := inputs[k.params.TargetUidInputName]; found {
		uidstr := uid.(string)
		k.addValues(values, uidstr, k.params.TargetPrefix)
	}
	if uid, found := inputs[k.params.OriginUidInputName]; found {
		uidstr := uid.(string)
		k.addValues(values, uidstr, k.params.OriginPrefix)
	}
	return values, nil
}

func (k *kubegen) addValues(vals map[string]interface{}, uid, valPrefix string) {
	podKey := keyFromUID(uid)
	pod, err := k.pods.GetPod(podKey)
	if err != nil {
		k.log.Warningf("error getting pod for (uid: %s, key: %s): %v", uid, podKey, err)
	}
	addPodValues(vals, valPrefix, k.params, pod)
}

func keyFromUID(uid string) string {
	if len(uid) == 0 {
		return ""
	}
	fullname := strings.TrimPrefix(uid, kubePrefix)
	if strings.Contains(fullname, ".") {
		parts := strings.Split(fullname, ".")
		if len(parts) == 2 {
			return fmt.Sprintf("%s/%s", parts[1], parts[0])
		}
	}
	return fullname
}

func addPodValues(m map[string]interface{}, prefix string, params config.Params, p *v1.Pod) {
	if p == nil {
		return
	}
	m[valueName(prefix, params.LabelsValueName)] = p.Labels
	m[valueName(prefix, params.PodNameValueName)] = p.Name
	m[valueName(prefix, params.NamespaceValueName)] = p.Namespace
	m[valueName(prefix, params.ServiceAccountValueName)] = p.Spec.ServiceAccountName
	m[valueName(prefix, params.PodIpValueName)] = p.Status.PodIP
	m[valueName(prefix, params.HostIpValueName)] = p.Status.HostIP
	if app, found := p.Labels[params.PodLabelForService]; found {
		m[valueName(prefix, params.ServiceValueName)] = app
	}
}

func valueName(prefix, value string) string {
	return fmt.Sprintf("%s%s", prefix, value)
}
