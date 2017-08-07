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
	"istio.io/pilot/adapter/config/crd"
	"istio.io/pilot/model"
	"istio.io/pilot/platform/kube"
	"istio.io/pilot/platform/kube/inject"
	"istio.io/pilot/test/util"
)

var (
	params infra

	// Enable/disable auth, or run both for the tests.
	authmode string
	verbose  bool
	count    int

	// The particular test to run, e.g. "HTTP reachability" or "routing rules"
	testType string

	kubeconfig string
	client     kubernetes.Interface
)

const (
	// CA image tag is the short SHA *update manually*
	caTag = "689b447"

	// Mixer image tag is the short SHA *update manually*
	mixerImage = "gcr.io/istio-testing/mixer:652be10fe0a6e001bf19993e4830365cf8018963"

	// retry budget
	budget = 90
)

func init() {
	flag.StringVar(&params.Hub, "hub", "gcr.io/istio-testing", "Docker hub")
	flag.StringVar(&params.Tag, "tag", "", "Docker tag")
	flag.StringVar(&params.CaImage, "ca", "gcr.io/istio-testing/istio-ca:"+caTag,
		"CA Docker image")
	flag.StringVar(&params.MixerImage, "mixer", mixerImage,
		"Mixer Docker image")
	flag.StringVar(&params.Namespace, "n", "",
		"Namespace to use for testing (empty to create/delete temporary one)")
	flag.BoolVar(&verbose, "verbose", false, "Debug level noise from proxies")
	flag.BoolVar(&params.checkLogs, "logs", true, "Validate pod logs (expensive in long-running tests)")

	flag.StringVar(&kubeconfig, "kubeconfig", "platform/kube/config",
		"kube config file (missing or empty file makes the test use in-cluster kube config instead)")
	flag.IntVar(&count, "count", 1, "Number of times to run the tests after deploying")
	flag.StringVar(&authmode, "auth", "both", "Enable / disable auth, or test both.")

	// If specified, only run one test
	flag.StringVar(&testType, "testtype", "", "Select test to run (default is all tests)")
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

	if err := setupClient(); err != nil {
		glog.Fatal(err)
	}

	params.Name = "(default infra)"
	params.Auth = proxyconfig.ProxyMeshConfig_NONE
	params.Mixer = true
	params.Ingress = true
	params.Egress = true
	params.Zipkin = true
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
	out.Auth = proxyconfig.ProxyMeshConfig_MUTUAL_TLS
	return out
}

func log(header, s string) {
	glog.Infof("\n\n"+
		"\033[1;34m=================== %s =====================\033[0m\n"+
		"\033[1;34m%s\033[0m\n\n", header, s)
}

func logError(header, s string) {
	glog.Errorf("\n\n"+
		"\033[1;31m=================== %s =====================\033[0m\n"+
		"\033[1;31m%s\033[0m\n\n", header, s)
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

		istio.apps, errs = util.GetAppPods(client, istio.Namespace)

		tests := []test{
			&http{infra: &istio},
			&grpc{infra: &istio},
			&tcp{infra: &istio},
			&ingress{infra: &istio},
			&egress{infra: &istio},
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
				if strings.HasPrefix(pod, "istio-pilot") {
					log("Discovery log", pod)
					glog.Info(util.FetchLogs(client, pod, istio.Namespace, "discovery"))
				} else if strings.HasPrefix(pod, "istio-mixer") {
					log("Mixer log", pod)
					glog.Info(util.FetchLogs(client, pod, istio.Namespace, "mixer"))
				} else {
					log("Proxy log", pod)
					glog.Info(util.FetchLogs(client, pod, istio.Namespace, inject.ProxyContainerName))
				}
			}
		}

		// always remove infra even if the tests fail
		log("Tearing down infrastructure", spew.Sdump(istio))
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

	tmpl, err := template.ParseFiles("test/integration/testdata/" + inFile)
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

// connect to K8S cluster and register TPRs
func setupClient() error {
	istioClient, err := crd.NewClient(kubeconfig, model.IstioConfigTypes, "dummy")
	if err != nil {
		return err
	}
	client, err = kube.CreateInterface(kubeconfig)
	if err != nil {
		return err
	}
	return istioClient.RegisterResources()
}
