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

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"ioutil"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"
	"github.com/golang/sync/errgroup"
	multierror "github.com/hashicorp/go-multierror"
	"k8s.io/client-go/kubernetes"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/istio/pilot/platform"
	"istio.io/istio/pilot/platform/kube"
	"istio.io/istio/pilot/platform/kube/inject"
	"istio.io/istio/pilot/test/util"
)

var (
	params infra

	// Enable/disable auth, or run both for the tests.
	authmode string
	// Enable/disable mixer
	mixermode string
	verbose   bool
	count     int

	// The particular test to run, e.g. "HTTP reachability" or "routing rules"
	testType string

	kubeconfig string
	client     kubernetes.Interface
)

const (
	// caImage specifies the default istio-ca docker image used for e2e testing *update manually*
	caImage = "gcr.io/istio-testing/istio-ca:2baec6baacecbd516ea0880573b6fc3cd5736739"

	// retry budget
	budget = 90

	mixerConfigFile     = "/etc/istio/proxy/envoy_mixer.json"
	mixerConfigAuthFile = "/etc/istio/proxy/envoy_mixer_auth.json"

	pilotConfigFile     = "/etc/istio/proxy/envoy_pilot.json"
	pilotConfigAuthFile = "/etc/istio/proxy/envoy_pilot_auth.json"
)

func init() {
	flag.StringVar(&params.Hub, "hub", "gcr.io/istio-testing", "Docker hub")
	flag.StringVar(&params.Tag, "tag", "", "Docker tag")
	flag.StringVar(&params.CaImage, "ca", caImage, "CA Docker image")
	flag.StringVar(&params.IstioNamespace, "ns", "",
		"Namespace in which to install Istio components (empty to create/delete temporary one)")
	flag.StringVar(&params.Namespace, "n", "",
		"Namespace in which to install the applications (empty to create/delete temporary one)")
	flag.StringVar(&params.Registry, "registry", string(platform.KubernetesRegistry), "Pilot registry")
	flag.BoolVar(&verbose, "verbose", false, "Debug level noise from proxies")
	flag.BoolVar(&params.checkLogs, "logs", true, "Validate pod logs (expensive in long-running tests)")

	flag.StringVar(&kubeconfig, "kubeconfig", "pilot/platform/kube/config",
		"kube config file (missing or empty file makes the test use in-cluster kube config instead)")
	flag.IntVar(&count, "count", 1, "Number of times to run the tests after deploying")
	flag.StringVar(&authmode, "auth", "both", "Enable / disable auth, or test both.")
	flag.StringVar(&mixermode, "mixer", "enable", "Enable / disable mixer.")
	flag.StringVar(&params.errorLogsDir, "errorlogsdir", "", "Store per pod logs as individual files in specific directory instead of writing to stderr.")

	// If specified, only run one test
	flag.StringVar(&testType, "testtype", "", "Select test to run (default is all tests)")

	// Keep disabled until default no-op initializer is distributed
	// and running in test clusters.
	flag.BoolVar(&params.UseInitializer, "use-initializer", false, "Use k8s sidecar initializer")
	flag.BoolVar(&params.UseAdmissionWebhook, "use-admission-webhook", false,
		"Use k8s external admission webhook for config validation")

	// TODO(github.com/kubernetes/kubernetes/issues/49987) - use
	// `istio-pilot-external` for the registered service name and
	// provide `istio-pilot` as --service-name argument to
	// platform/kube/admit/webhook-workaround.sh. Once this bug is
	// fixed (and for non-GKE k8s) the admission-service-name should
	// be `istio-pilot`.
	flag.StringVar(&params.AdmissionServiceName, "admission-service-name", "istio-pilot-external",
		"Name of admission webhook service name")

	flag.IntVar(&params.DebugPort, "debugport", 0, "Debugging port")

	flag.BoolVar(&params.debugImagesAndMode, "debug", true, "Use debug images and mode (false for prod)")

}

type test interface {
	String() string
	setup() error
	run() error
	teardown()
}

func main() {
	flag.Parse()
	if params.Tag == "" {
		glog.Fatal("No docker tag specified")
	}

	if verbose {
		params.Verbosity = 3
	} else {
		params.Verbosity = 2
	}

	params.Name = "(default infra)"
	params.Auth = proxyconfig.MeshConfig_NONE
	params.Mixer = true
	params.Ingress = true
	params.Zipkin = true
	params.MixerCustomConfigFile = mixerConfigFile
	params.PilotCustomConfigFile = pilotConfigFile
	params.errorLogsDir
	if mixermode == "disable" {
		params.Mixer = false
	}

	if len(params.Namespace) != 0 && authmode == "both" {
		glog.Infof("When namespace(=%s) is specified, auth mode(=%s) must be one of enable or disable.",
			params.Namespace, authmode)
		return
	}

	var err error
	_, client, err = kube.CreateInterface(kubeconfig)
	if err != nil {
		glog.Fatal(err)
	}

	switch authmode {
	case "enable":
		runTests(setAuth(params))
	case "disable":
		runTests(params)
	case "both":
		runTests(params, setAuth(params))
	default:
		glog.Infof("Invald auth flag: %s. Please choose from: enable/disable/both.", authmode)
	}
}

func setAuth(params infra) infra {
	out := params
	out.Name = "(auth infra)"
	out.Auth = proxyconfig.MeshConfig_MUTUAL_TLS
	out.ControlPlaneAuthPolicy = proxyconfig.AuthenticationPolicy_MUTUAL_TLS
	out.MixerCustomConfigFile = mixerConfigAuthFile
	out.PilotCustomConfigFile = pilotConfigAuthFile
	return out
}

func log(header, s string) {
	glog.Infof("\n\n=================== %s =====================\n%s\n\n", header, s)
}

func logError(header, s string) {
	glog.Errorf("\n\n=================== %s =====================\n%s\n\n", header, s)
}

func runTests(envs ...infra) {
	var result error
	for _, istio := range envs {
		var errs error
		log("Deploying infrastructure", spew.Sdump(istio))
		if err := istio.setup(); err != nil {
			result = multierror.Append(result, err)
			continue
		}
		if err := istio.deployApps(); err != nil {
			result = multierror.Append(result, err)
			continue
		}

		nslist := []string{istio.IstioNamespace, istio.Namespace}
		istio.apps, errs = util.GetAppPods(client, nslist)
		if errs != nil {
			result = multierror.Append(result, errs)
			break
		}

		tests := []test{
			&http{infra: &istio},
			&grpc{infra: &istio},
			&tcp{infra: &istio},
			&headless{infra: &istio},
			&ingress{infra: &istio},
			&egressRules{infra: &istio},
			&routing{infra: &istio},
			&zipkin{infra: &istio},
		}

		for _, test := range tests {
			// If the user has specified a test, skip all other tests
			if len(testType) > 0 && testType != test.String() {
				continue
			}

			for i := 0; i < count; i++ {
				log("Test run", strconv.Itoa(i))
				if err := test.setup(); err != nil {
					errs = multierror.Append(errs, multierror.Prefix(err, test.String()))
				} else {
					log("Running test", test.String())
					if err := test.run(); err != nil {
						errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("%v run %d", test, i)))
					} else {
						log("Success!", test.String())
					}
				}
				log("Tearing down test", test.String())
				test.teardown()
			}
		}

		// spill all logs on error
		if errs != nil {
			for _, pod := range util.GetPods(client, istio.Namespace) {
				var filename, content string
				if strings.HasPrefix(pod, "istio-pilot") {
					log("Discovery log", pod)
					filename = "istio-pilot"
					content = util.FetchLogs(client, pod, istio.IstioNamespace, "discovery")
				} else if strings.HasPrefix(pod, "istio-mixer") {
					log("Mixer log", pod)
					filename = "istio-mixer"
					content = util.FetchLogs(client, pod, istio.IstioNamespace, "mixer")
				} else if strings.HasPrefix(pod, "istio-ingress") {
					log("Ingress log", pod)
					filename = "istio-ingress"
					content = util.FetchLogs(client, pod, istio.IstioNamespace, inject.ProxyContainerName)
				} else {
					log("Proxy log", pod)
					filename = pod
					content = util.FetchLogs(client, pod, istio.Namespace, inject.ProxyContainerName)
				}

				if len(istio.errorLogsDir) > 0 {
					if err := ioutil.WriteFile(istio.errorLogsDir+"/"+filename+".txt", []byte(content), 0644); err != nil {
						glog.Errorf("Failed to save logs to %s:%s. Dumping on stderr\n", filename, err)
						glog.Info(content)
					}
				} else {
					glog.Info(content)
				}
			}
		}

		// always remove infra even if the tests fail
		log("Tearing down infrastructure", istio.Name)
		istio.teardown()

		if errs == nil {
			log("Passed all tests!", fmt.Sprintf("tests: %v, count: %d", tests, count))
		} else {
			logError("Failed tests!", errs.Error())
			result = multierror.Append(result, multierror.Prefix(errs, istio.Name))
		}
	}

	if result == nil {
		log("Passed infrastructure tests!", spew.Sdump(envs))
	} else {
		logError("Failed infrastructure tests!", result.Error())
		os.Exit(1)
	}
}

// fill a file based on a template
func fill(inFile string, values interface{}) (string, error) {
	var bytes bytes.Buffer
	w := bufio.NewWriter(&bytes)

	tmpl, err := template.ParseFiles("pilot/test/integration/testdata/" + inFile)
	if err != nil {
		return "", err
	}

	if err := tmpl.Execute(w, values); err != nil {
		return "", err
	}

	if err := w.Flush(); err != nil {
		return "", err
	}

	return bytes.String(), nil
}

type status error

var (
	errAgain status = errors.New("try again")
)

// run in parallel with retries. all funcs must succeed for the function to succeed
func parallel(fs map[string]func() status) error {
	g, ctx := errgroup.WithContext(context.Background())
	repeat := func(name string, f func() status) func() error {
		return func() error {
			for n := 0; n < budget; n++ {
				glog.Infof("%s (attempt %d)", name, n)
				err := f()
				switch err {
				case nil:
					// success
					return nil
				case errAgain:
					// do nothing
				default:
					return fmt.Errorf("failed %s at attempt %d: %v", name, n, err)
				}
				select {
				case <-time.After(time.Second):
					// try again
				case <-ctx.Done():
					return nil
				}
			}
			return fmt.Errorf("failed all %d attempts for %s", budget, name)
		}
	}
	for name, f := range fs {
		g.Go(repeat(name, f))
	}
	return g.Wait()
}

// repeat a check up to budget until it does not return an error
func repeat(f func() error, budget int, delay time.Duration) error {
	var errs error
	for i := 0; i < budget; i++ {
		err := f()
		if err == nil {
			return nil
		}
		errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("attempt %d", i)))
		glog.Infof("attempt #%d failed with %v", i, err)
		time.Sleep(delay)
	}
	return errs
}
