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

package bugreport

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/slices"
	config2 "istio.io/istio/tools/bug-report/pkg/config"
)

var (
	startTime, endTime, configFile, tempDir, outputDir string
	included, excluded                                 []string
	commandTimeout, since                              time.Duration
	gConfig                                            = &config2.BugReportConfig{}
)

const defaultProxyAdminPort = 15000

func addFlags(cmd *cobra.Command, args *config2.BugReportConfig) {
	// k8s client config
	cmd.PersistentFlags().StringVarP(&args.KubeConfigPath, "kubeconfig", "c", "",
		"Path to kube config.")
	cmd.PersistentFlags().StringVar(&args.Context, "context", "",
		"Name of the kubeconfig Context to use.")

	// input config
	cmd.PersistentFlags().StringVarP(&configFile, "filename", "f", "",
		"Path to a file containing configuration in YAML format. The file contents are applied over the default "+
			"values and flag settings, with lists being replaced per JSON merge semantics.")

	// dry run
	cmd.PersistentFlags().BoolVarP(&args.DryRun, "dry-run", "", false,
		"Only log commands that would be run, don't fetch or write.")

	cmd.PersistentFlags().IntVar(&args.ProxyAdminPort, "proxy-admin-port", defaultProxyAdminPort,
		"Envoy proxy admin port")

	// full secrets
	cmd.PersistentFlags().BoolVarP(&args.FullSecrets, "full-secrets", "", false,
		"If set, secret contents are included in output.")

	// istio namespaces
	cmd.PersistentFlags().StringVar(&args.IstioNamespace, "istio-namespace", bugReportDefaultIstioNamespace,
		"Namespace where Istio control plane is installed.")

	// timeouts and max sizes
	cmd.PersistentFlags().DurationVar(&commandTimeout, "timeout", bugReportDefaultTimeout,
		"Maximum amount of time to spend fetching logs. When timeout is reached "+
			"only the logs captured so far are saved to the archive.")
	// include / exclude specs
	cmd.PersistentFlags().StringSliceVar(&included, "include", bugReportDefaultInclude,
		"Spec for which pod's proxy logs to include in the archive. See above for format and examples.")
	cmd.PersistentFlags().StringSliceVar(&excluded, "exclude", bugReportDefaultExclude,
		"Spec for which pod's proxy logs to exclude from the archive, after the include spec "+
			"is processed. See above for format and examples.")

	// log time ranges
	cmd.PersistentFlags().StringVar(&startTime, "start-time", "",
		"Start time for the range of log entries to include in the archive. "+
			"Default is the infinite past. If set, --duration must be unset.")
	cmd.PersistentFlags().StringVar(&endTime, "end-time", "",
		"End time for the range of log entries to include in the archive. Default is now.")
	cmd.PersistentFlags().DurationVar(&since, "duration", 0,
		"How far to go back in time from end-time for log entries to include in the archive. "+
			"Default is infinity. If set, --start-time must be unset.")

	// log error control
	cmd.PersistentFlags().StringSliceVar(&args.CriticalErrors, "critical-errs", nil,
		"List of comma separated glob patterns to match against log error strings. "+
			"If any pattern matches an error in the log, the logs is given the highest priority for archive inclusion.")
	cmd.PersistentFlags().StringSliceVar(&args.IgnoredErrors, "ignore-errs", nil,
		"List of comma separated glob patterns to match against log error strings. "+
			"Any error matching these patterns is ignored when calculating the log importance heuristic.")

	// working dir to store temporary artifacts
	cmd.PersistentFlags().StringVar(&tempDir, "dir", "",
		"Set a specific directory for temporary artifact storage.")

	cmd.PersistentFlags().StringVar(&outputDir, "output-dir", "",
		"Set a specific directory for output archive file.")

	// in-flight request limit
	cmd.PersistentFlags().IntVar(&args.RequestConcurrency, "rq-concurrency", 0,
		"Set the concurrency limit of requests to the Kubernetes API server, defaults to 32.")
}

func parseConfig() (*config2.BugReportConfig, error) {
	fileConfig := &config2.BugReportConfig{}
	if configFile != "" {
		b, err := os.ReadFile(configFile)
		if err != nil {
			return nil, err
		}
		if err := yaml.Unmarshal(b, fileConfig); err != nil {
			return nil, err
		}
	}

	if err := parseTimes(gConfig, startTime, endTime, since); err != nil {
		return nil, err
	}
	gConfig.CommandTimeout = config2.Duration(commandTimeout)
	for _, s := range included {
		ss := &config2.SelectionSpec{}
		if err := ss.UnmarshalJSON([]byte(s)); err != nil {
			return nil, err
		}
		gConfig.Include = append(gConfig.Include, ss)
	}
	// Exclude default system namespaces eg: bugReportDefaultExclude
	dss := &config2.SelectionSpec{}
	if err := dss.UnmarshalJSON([]byte(bugReportDefaultExclude[0])); err != nil {
		return nil, err
	}
	gConfig.Exclude = append(gConfig.Exclude, dss)
	// Exclude namespace provided by --exclude flag
	for _, s := range excluded {
		ess := &config2.SelectionSpec{}
		if err := ess.UnmarshalJSON([]byte(s)); err != nil {
			return nil, err
		}
		ess.Namespaces = slices.FilterInPlace(ess.Namespaces, func(ns string) bool { return !inject.IgnoredNamespaces.Contains(ns) })
		if len(ess.Namespaces) > 0 {
			gConfig.Exclude = append(gConfig.Exclude, ess)
		}
	}
	return overlayConfig(fileConfig, gConfig)
}

func parseTimes(config *config2.BugReportConfig, startTime, endTime string, duration time.Duration) error {
	if startTime == "" && endTime == "" {
		config.TimeFilterApplied = false
	} else {
		config.TimeFilterApplied = true
	}

	config.EndTime = time.Now()
	config.Since = config2.Duration(duration)
	if endTime != "" {
		var err error
		config.EndTime, err = time.Parse(time.RFC3339, endTime)
		if err != nil {
			return fmt.Errorf("bad format for end-time: %s, expect RFC3339 e.g. %s", endTime, time.RFC3339)
		}
	}
	if config.Since != 0 {
		if startTime != "" {
			return fmt.Errorf("only one --start-time or --duration may be set")
		}
		config.StartTime = config.EndTime.Add(-1 * time.Duration(config.Since))
	} else {
		var err error
		if startTime == "" {
			config.StartTime = time.Time{}
		} else {
			config.StartTime, err = time.Parse(time.RFC3339, startTime)
			if err != nil {
				return fmt.Errorf("bad format for start-time: %s, expect RFC3339 e.g. %s", startTime, time.RFC3339)
			}
			if config.StartTime.After(config.EndTime) {
				return fmt.Errorf("bad format for start-time and end-time: start-time is after end-time")
			}
		}
	}
	return nil
}

func overlayConfig(base, overlay *config2.BugReportConfig) (*config2.BugReportConfig, error) {
	bj, err := json.Marshal(base)
	if err != nil {
		return nil, err
	}
	oj, err := json.Marshal(overlay)
	if err != nil {
		return nil, err
	}

	mj, err := jsonpatch.MergePatch(bj, oj)
	if err != nil {
		return nil, fmt.Errorf("json merge error (%s) for base object: \n%s\n override object: \n%s", err, bj, oj)
	}

	out := &config2.BugReportConfig{}
	err = json.Unmarshal(mj, out)
	return out, err
}
