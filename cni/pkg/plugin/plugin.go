// Copyright Istio Authors
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

// This is a sample chained plugin that supports multiple CNI versions. It
// parses prevResult according to the cniVersion
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	cniv1 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/cni/pkg/constants"
	"istio.io/istio/cni/pkg/util"
	"istio.io/istio/pkg/file"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
)

var (
	injectAnnotationKey = annotation.SidecarInject.Name
	sidecarStatusKey    = annotation.SidecarStatus.Name

	podRetrievalMaxRetries = 30
	podRetrievalInterval   = 1 * time.Second
)

const (
	ISTIOINIT  = "istio-init"
	ISTIOPROXY = "istio-proxy"
)

// Config is whatever you expect your configuration json to be. This is whatever
// is passed in on stdin. Your plugin may wish to expose its functionality via
// runtime args, see CONVENTIONS.md in the CNI spec.
type Config struct {
	types.NetConf

	// Add plugin-specific flags here
	PluginLogLevel      string                    `json:"plugin_log_level"`
	CNIAgentRunDir      string                    `json:"cni_agent_run_dir"`
	AmbientEnabled      bool                      `json:"ambient_enabled"`
	EnablementSelectors []util.EnablementSelector `json:"enablement_selectors"`
	ExcludeNamespaces   []string                  `json:"exclude_namespaces"`
	PodNamespace        string                    `json:"pod_namespace"`
}

// K8sArgs is the valid CNI_ARGS used for Kubernetes
// The field names need to match exact keys in containerd args for unmarshalling
// https://github.com/containerd/containerd/blob/ced9b18c231a28990617bc0a4b8ce2e81ee2ffa1/pkg/cri/server/sandbox_run.go#L526-L532
type K8sArgs struct {
	types.CommonArgs
	K8S_POD_NAME               types.UnmarshallableString // nolint: revive, stylecheck
	K8S_POD_NAMESPACE          types.UnmarshallableString // nolint: revive, stylecheck
	K8S_POD_INFRA_CONTAINER_ID types.UnmarshallableString // nolint: revive, stylecheck
	K8S_POD_UID                types.UnmarshallableString // nolint: revive, stylecheck
}

// parseConfig parses the supplied configuration (and prevResult) from stdin.
func parseConfig(stdin []byte) (*Config, error) {
	conf := Config{}

	if err := json.Unmarshal(stdin, &conf); err != nil {
		return nil, fmt.Errorf("failed to parse network configuration: %v", err)
	}

	log.Debugf("istio-cni: Config is: %+v", conf)
	// Parse previous result. Remove this if your plugin is not chained.
	if conf.RawPrevResult != nil {
		resultBytes, err := json.Marshal(conf.RawPrevResult)
		if err != nil {
			return nil, fmt.Errorf("could not serialize prevResult: %v", err)
		}
		res, err := version.NewResult(conf.CNIVersion, resultBytes)
		if err != nil {
			return nil, fmt.Errorf("could not parse prevResult: %v", err)
		}
		conf.RawPrevResult = nil
		conf.PrevResult, err = cniv1.NewResultFromResult(res)
		if err != nil {
			return nil, fmt.Errorf("could not convert result to current version: %v", err)
		}
	}
	// End previous result parsing

	return &conf, nil
}

// Logging with CNI plugins is special - we *cannot* log to stdout, as the CNI spec uses stdin/stdout to pass context between invoked plugins.
// So, we log to a rolling logfile, and also forward logs via UDS to the node agent (if available)
func GetLoggingOptions(cfg *Config) *log.Options {
	loggingOptions := log.DefaultOptions()
	loggingOptions.OutputPaths = []string{"stderr"}
	loggingOptions.JSONEncoding = true
	if cfg != nil {

		udsAddr := filepath.Join(cfg.CNIAgentRunDir, constants.LogUDSSocketName)
		// Tee all logs to UDS. Stdout will go to kubelet (hard to access, UDS will be read by the CNI DaemonSet and exposed
		// by normal `kubectl logs`
		if file.Exists(udsAddr) {
			loggingOptions.WithTeeToUDS(udsAddr, constants.UDSLogPath)
		}

		// Also tee to a rolling log on the node's local filesystem, in case the UDS server is down.
		loggingOptions.WithTeeToRollingLocal(filepath.Join(cfg.CNIAgentRunDir, constants.LocalRollingLogName), constants.RollingLogMaxSizeMB)

		// Override plugin log level based on their config. Not we use "all" (OverrideScopeName) since there is no scoping in the plugin.
		if cfg.PluginLogLevel != "" {
			loggingOptions.SetDefaultOutputLevel(log.OverrideScopeName, log.StringToLevel(cfg.PluginLogLevel))
		}
	}
	return loggingOptions
}

// CmdAdd is called for ADD requests
func CmdAdd(args *skel.CmdArgs) (err error) {
	// Defer a panic recover, so that in case if panic we can still return
	// a proper error to the runtime.
	defer func() {
		if e := recover(); e != nil {
			msg := fmt.Sprintf("istio-cni panicked during cmdAdd: %v\n%v", e, string(debug.Stack()))
			if err != nil {
				// If we're recovering and there was also an error, then we need to
				// present both.
				msg = fmt.Sprintf("%s: %v", msg, err)
			}
			err = errors.New(msg)
		}
		if err != nil {
			log.Errorf("istio-cni cmdAdd error: %v", err)
		}
	}()

	conf, err := parseConfig(args.StdinData)
	if err != nil {
		log.Errorf("istio-cni cmdAdd failed to parse config %v %v", string(args.StdinData), err)
		return err
	}

	// Create a kube client
	client, err := newK8sClient(*conf)
	if err != nil {
		return err
	}

	// Actually do the add
	if err := doAddRun(args, conf, client, IptablesInterceptRuleMgr()); err != nil {
		return err
	}
	return pluginResponse(conf)
}

func doAddRun(args *skel.CmdArgs, conf *Config, kClient kubernetes.Interface, rulesMgr InterceptRuleMgr) error {
	if err := log.Configure(GetLoggingOptions(conf)); err != nil {
		log.Error("Failed to configure istio-cni logging")
	}

	var loggedPrevResult any
	if conf.PrevResult == nil {
		loggedPrevResult = "none"
	} else {
		loggedPrevResult = conf.PrevResult
	}
	log.WithLabels("if", args.IfName).Debugf("istio-cni CmdAdd config: %+v", conf)
	log.Debugf("istio-cni CmdAdd previous result: %+v", loggedPrevResult)

	// Determine if running under k8s by checking the CNI args
	k8sArgs := K8sArgs{}
	if err := types.LoadArgs(args.Args, &k8sArgs); err != nil {
		return err
	}

	// Check if the workload is running under Kubernetes.
	podNamespace := string(k8sArgs.K8S_POD_NAMESPACE)
	podName := string(k8sArgs.K8S_POD_NAME)
	log := log.WithLabels("pod", podNamespace+"/"+podName)
	if podNamespace == "" || podName == "" {
		log.Debugf("Not a kubernetes pod")
		return nil
	}

	for _, excludeNs := range conf.ExcludeNamespaces {
		if podNamespace == excludeNs {
			log.Infof("pod namespace excluded")
			return nil
		}
	}

	// Begin ambient plugin logic
	// For ambient pods, this is all the logic we need to run
	if conf.AmbientEnabled {
		log.Debugf("istio-cni ambient cmdAdd podName: %s - checking if ambient enabled", podName)
		podIsAmbient, err := isAmbientPod(kClient, podName, podNamespace, conf.EnablementSelectors)
		if err != nil {
			log.Errorf("istio-cni cmdAdd failed to check ambient: %s", err)
		}

		var prevResIps []*cniv1.IPConfig
		if conf.PrevResult != nil {
			prevResult := conf.PrevResult.(*cniv1.Result)
			prevResIps = prevResult.IPs
		}

		// Only send event if this pod "would be" an ambient-watched pod - otherwise skip
		if podIsAmbient {
			cniEventAddr := filepath.Join(conf.CNIAgentRunDir, constants.CNIEventSocketName)
			cniClient := newCNIClient(cniEventAddr, constants.CNIAddEventPath)
			if err = PushCNIEvent(cniClient, args, prevResIps, podName, podNamespace); err != nil {
				// return a more informative error in the pod event log if CNI plugin fails
				wrapErr := fmt.Errorf("istio-cni cmdAdd failed to contact node Istio CNI agent: %s", err)
				return wrapErr
			}
			return nil
		}
		log.Debugf("istio-cni ambient cmdAdd podName: %s - not ambient enabled, ignoring", podName)
	}
	// End ambient plugin logic

	maybeCNIPod := string(k8sArgs.K8S_POD_NAME)
	maybeCNINS := string(k8sArgs.K8S_POD_NAMESPACE)
	pi := &PodInfo{}
	var k8sErr error
	for attempt := 1; attempt <= podRetrievalMaxRetries; attempt++ {
		pi, k8sErr = getK8sPodInfo(kClient, podName, podNamespace)
		if k8sErr == nil {
			break
		}
		log.Debugf("Failed to get pod info: %v", k8sErr)

		// Failsafe - if we get here, we could be in a state where
		// 1. We are being upgraded - `istio-cni` node agent pod is gone
		// 2. This plugin was left in place to stall pod spawns until the
		// replacement arrives.
		// 3. This plugin can't contact the K8S API server (creds expired/invalid)
		// 4. The pod this plugin would be blocking by returning this error
		// *is* our replacement `istio-cni` pod (which would refresh our creds)
		//
		// So, if we can't contact the K8S API server at all, fall back to checking the
		// K8S_POD/K8S_NAMESPACE values from the CNI layer, and let this pod through
		// if it looks like it might be our `istio-cni` node agent.
		//
		// We could do this check unconditionally above, but it seems smarter to only
		// fall back to this (lightly) relaxed check when we know we are in a degraded state.
		//
		// Is this fail open? Not really, the K8S args come from the cluster's CNI and are as-authoritative
		// as the hard query we would otherwise make against the API.
		//
		// TODO NRI could probably give us more identifying information here OOB from k8s.
		if strings.HasPrefix(maybeCNIPod, "istio-cni-node-") &&
			maybeCNINS == conf.PodNamespace {
			log.Infof("in a degraded state and %v looks like our own agent pod, skipping", maybeCNIPod)
			return nil
		}

		time.Sleep(podRetrievalInterval)
	}

	if k8sErr != nil {
		log.Errorf("Failed to get pod info: %v", k8sErr)
		return k8sErr
	}

	// Check if istio-init container is present; in that case exclude pod
	if pi.Containers.Contains(ISTIOINIT) {
		log.Infof("excluded due to being already injected with istio-init container")
		return nil
	}

	if val, ok := pi.ProxyEnvironments["DISABLE_ENVOY"]; ok {
		if val, err := strconv.ParseBool(val); err == nil && val {
			log.Infof("excluded due to DISABLE_ENVOY on istio-proxy")
			return nil
		}
	}

	if !pi.Containers.Contains(ISTIOPROXY) {
		log.Infof("excluded because it does not have istio-proxy container (have %v)", sets.SortedList(pi.Containers))
		return nil
	}

	if pi.ProxyType != "" && pi.ProxyType != "sidecar" {
		log.Infof("excluded %s/%s pod because it has proxy type %s", podNamespace, podName, pi.ProxyType)
		return nil
	}

	val := pi.Annotations[injectAnnotationKey]
	if lbl, labelPresent := pi.Labels[label.SidecarInject.Name]; labelPresent {
		// The label is the new API; if both are present we prefer the label
		val = lbl
	}
	if val != "" {
		log.Debugf("contains inject annotation: %s", val)
		if injectEnabled, err := strconv.ParseBool(val); err == nil {
			if !injectEnabled {
				log.Infof("excluded due to inject-disabled annotation")
				return nil
			}
		}
	}

	if _, ok := pi.Annotations[sidecarStatusKey]; !ok {
		log.Infof("excluded due to not containing sidecar annotation")
		return nil
	}

	log.Debugf("Setting up redirect")

	redirect, err := NewRedirect(pi)
	if err != nil {
		log.Errorf("redirect failed due to bad params: %v", err)
		return err
	}

	if err := rulesMgr.Program(podName, args.Netns, redirect); err != nil {
		return err
	}

	return nil
}

func pluginResponse(conf *Config) error {
	var result *cniv1.Result
	if conf.PrevResult == nil {
		result = &cniv1.Result{
			CNIVersion: cniv1.ImplementedSpecVersion,
		}
		return types.PrintResult(result, conf.CNIVersion)
	}

	// Pass through the result for the next plugin
	return types.PrintResult(conf.PrevResult, conf.CNIVersion)
}

func CmdCheck(args *skel.CmdArgs) (err error) {
	return nil
}

func CmdDelete(args *skel.CmdArgs) (err error) {
	return nil
}

func isAmbientPod(client kubernetes.Interface, podName, podNamespace string, selectors []util.EnablementSelector) (bool, error) {
	compiledSelectors, err := util.NewCompiledEnablementSelectors(selectors)
	if err != nil {
		return false, fmt.Errorf("failed to instantiate ambient enablement selector: %v", err)
	}

	pod, err := client.CoreV1().Pods(podNamespace).Get(context.Background(), podName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	ns, err := client.CoreV1().Namespaces().Get(context.Background(), podNamespace, metav1.GetOptions{})
	if err != nil {
		return false, err
	}

	return compiledSelectors.Matches(pod.Labels, pod.Annotations, ns.Labels), nil
}
