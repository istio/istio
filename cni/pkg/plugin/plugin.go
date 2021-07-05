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

// This is a sample chained plugin that supports multiple CNI versions. It
// parses prevResult according to the cniVersion
package plugin

import (
	"encoding/json"
	"fmt"
	"net"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/cni/pkg/version"

	"istio.io/api/annotation"
	"istio.io/pkg/log"
)

var (
	nsSetupBinDir       = "/opt/cni/bin"
	injectAnnotationKey = annotation.SidecarInject.Name
	sidecarStatusKey    = annotation.SidecarStatus.Name

	interceptRuleMgrType   = defInterceptRuleMgrType
	podRetrievalMaxRetries = 30
	podRetrievalInterval   = 1 * time.Second
)

const ISTIOINIT = "istio-init"

// Kubernetes a K8s specific struct to hold config
type Kubernetes struct {
	K8sAPIRoot           string   `json:"k8s_api_root"`
	Kubeconfig           string   `json:"kubeconfig"`
	InterceptRuleMgrType string   `json:"intercept_type"`
	NodeName             string   `json:"node_name"`
	ExcludeNamespaces    []string `json:"exclude_namespaces"`
	CNIBinDir            string   `json:"cni_bin_dir"`
}

// Config is whatever you expect your configuration json to be. This is whatever
// is passed in on stdin. Your plugin may wish to expose its functionality via
// runtime args, see CONVENTIONS.md in the CNI spec.
type Config struct {
	types.NetConf           // You may wish to not nest this type
	RuntimeConfig *struct { // SampleConfig map[string]interface{} `json:"sample"`
	} `json:"runtimeConfig"`

	// This is the previous result, when called in the context of a chained
	// plugin. Because this plugin supports multiple versions, we'll have to
	// parse this in two passes. If your plugin is not chained, this can be
	// removed (though you may wish to error if a non-chainable plugin is
	// chained.
	// If you need to modify the result before returning it, you will need
	// to actually convert it to a concrete versioned struct.
	RawPrevResult *map[string]interface{} `json:"prevResult"`
	PrevResult    *current.Result         `json:"-"`

	// Add plugin-specific flags here
	LogLevel   string     `json:"log_level"`
	Kubernetes Kubernetes `json:"kubernetes"`
}

// K8sArgs is the valid CNI_ARGS used for Kubernetes
// The field names need to match exact keys in kubelet args for unmarshalling
type K8sArgs struct {
	types.CommonArgs
	IP                         net.IP
	K8S_POD_NAME               types.UnmarshallableString // nolint: golint, stylecheck
	K8S_POD_NAMESPACE          types.UnmarshallableString // nolint: golint, stylecheck
	K8S_POD_INFRA_CONTAINER_ID types.UnmarshallableString // nolint: golint, stylecheck
}

// parseConfig parses the supplied configuration (and prevResult) from stdin.
func parseConfig(stdin []byte) (*Config, error) {
	conf := Config{}

	if err := json.Unmarshal(stdin, &conf); err != nil {
		return nil, fmt.Errorf("failed to parse network configuration: %v", err)
	}

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
		conf.PrevResult, err = current.NewResultFromResult(res)
		if err != nil {
			return nil, fmt.Errorf("could not convert result to current version: %v", err)
		}
	}
	// End previous result parsing

	return &conf, nil
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
			err = fmt.Errorf(msg)
		}
		if err != nil {
			log.Errorf("istio-cni cmdAdd error: %v", err)
		}
	}()

	conf, err := parseConfig(args.StdinData)
	if err != nil {
		log.Errorf("istio-cni cmdAdd parsing config %v", err)
		return err
	}

	var loggedPrevResult interface{}
	if conf.PrevResult == nil {
		loggedPrevResult = "none"
	} else {
		loggedPrevResult = conf.PrevResult
	}

	log.WithLabels("version", conf.CNIVersion, "prevResult", loggedPrevResult).Info("CmdAdd config parsed")

	// Determine if running under k8s by checking the CNI args
	k8sArgs := K8sArgs{}
	if err := types.LoadArgs(args.Args, &k8sArgs); err != nil {
		return err
	}
	log.Infof("Getting identifiers with arguments: %s", args.Args)
	log.Infof("Loaded k8s arguments: %v", k8sArgs)
	if conf.Kubernetes.CNIBinDir != "" {
		nsSetupBinDir = conf.Kubernetes.CNIBinDir
	}
	if conf.Kubernetes.InterceptRuleMgrType != "" {
		interceptRuleMgrType = conf.Kubernetes.InterceptRuleMgrType
	}

	log.WithLabels("ContainerID", args.ContainerID, "Pod", string(k8sArgs.K8S_POD_NAME),
		"Namespace", string(k8sArgs.K8S_POD_NAMESPACE), "InterceptType", interceptRuleMgrType).Info("")

	// Check if the workload is running under Kubernetes.
	if string(k8sArgs.K8S_POD_NAMESPACE) != "" && string(k8sArgs.K8S_POD_NAME) != "" {
		excludePod := false
		for _, excludeNs := range conf.Kubernetes.ExcludeNamespaces {
			if string(k8sArgs.K8S_POD_NAMESPACE) == excludeNs {
				excludePod = true
				break
			}
		}
		if !excludePod {
			client, err := newKubeClient(*conf)
			if err != nil {
				return err
			}
			log.Debugf("Created Kubernetes client: %v", client)
			pi := &PodInfo{}
			var k8sErr error
			for attempt := 1; attempt <= podRetrievalMaxRetries; attempt++ {
				pi, k8sErr = getKubePodInfo(client, string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE))
				if k8sErr == nil {
					break
				}
				log.WithLabels("err", k8sErr, "attempt", attempt).Warn("Waiting for pod metadata")
				time.Sleep(podRetrievalInterval)
			}
			if k8sErr != nil {
				log.WithLabels("err", k8sErr).Error("Failed to get pod data")
				return k8sErr
			}

			// Check if istio-init container is present; in that case exclude pod
			if _, present := pi.InitContainers[ISTIOINIT]; present {
				log.WithLabels(
					"pod", string(k8sArgs.K8S_POD_NAME),
					"namespace", string(k8sArgs.K8S_POD_NAMESPACE)).
					Info("Pod excluded due to being already injected with istio-init container")
				excludePod = true
			}

			if val, ok := pi.ProxyEnvironments["DISABLE_ENVOY"]; ok {
				if val, err := strconv.ParseBool(val); err == nil && val {
					log.Infof("Pod excluded due to DISABLE_ENVOY on istio-proxy")
					excludePod = true
				}
			}

			log.Infof("Found containers %v", pi.Containers)
			if len(pi.Containers) > 1 {
				log.WithLabels(
					"ContainerID", args.ContainerID,
					"netns", args.Netns,
					"pod", string(k8sArgs.K8S_POD_NAME),
					"Namespace", string(k8sArgs.K8S_POD_NAMESPACE),
					"annotations", pi.Annotations).
					Info("Checking annotations prior to redirect for Istio proxy")
				if val, ok := pi.Annotations[injectAnnotationKey]; ok {
					log.Infof("Pod %s contains inject annotation: %s", string(k8sArgs.K8S_POD_NAME), val)
					if injectEnabled, err := strconv.ParseBool(val); err == nil {
						if !injectEnabled {
							log.Infof("Pod excluded due to inject-disabled annotation")
							excludePod = true
						}
					}
				}
				if _, ok := pi.Annotations[sidecarStatusKey]; !ok {
					log.Infof("Pod %s excluded due to not containing sidecar annotation", string(k8sArgs.K8S_POD_NAME))
					excludePod = true
				}
				if !excludePod {
					log.Infof("setting up redirect")
					if redirect, redirErr := NewRedirect(pi); redirErr != nil {
						log.Errorf("Pod redirect failed due to bad params: %v", redirErr)
					} else {
						log.Infof("Redirect local ports: %v", redirect.includePorts)
						// Get the constructor for the configured type of InterceptRuleMgr
						interceptMgrCtor := GetInterceptRuleMgrCtor(interceptRuleMgrType)
						if interceptMgrCtor == nil {
							log.Errorf("Pod redirect failed due to unavailable InterceptRuleMgr of type %s",
								interceptRuleMgrType)
						} else {
							rulesMgr := interceptMgrCtor()
							if err := rulesMgr.Program(args.Netns, redirect); err != nil {
								return err
							}
						}
					}
				}
			}
		} else {
			log.Infof("Pod excluded")
		}
	} else {
		log.Infof("No Kubernetes Data")
	}

	var result *current.Result
	if conf.PrevResult == nil {
		result = &current.Result{
			CNIVersion: current.ImplementedSpecVersion,
		}
	} else {
		// Pass through the result for the next plugin
		result = conf.PrevResult
	}
	return types.PrintResult(result, conf.CNIVersion)
}

func CmdCheck(args *skel.CmdArgs) (err error) {
	return nil
}

func CmdDelete(args *skel.CmdArgs) (err error) {
	return nil
}
