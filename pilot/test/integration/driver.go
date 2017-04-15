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
	"flag"
	"fmt"
	"os"
	"text/template"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"
	"github.com/golang/sync/errgroup"
	multierror "github.com/hashicorp/go-multierror"
	"k8s.io/client-go/kubernetes"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/manager/model"
	"istio.io/manager/platform/kube"
	"istio.io/manager/test/util"
)

const (
	// CA image tag is the short SHA *update manually*
	caTag = "f063b41"

	// Mixer image tag is the short SHA *update manually*
	mixerTag = "6655a67"
)

type parameters struct {
	infra      infra
	kubeconfig string
	count      int
	debug      bool
	logs       bool
}

var (
	params parameters

	client      kubernetes.Interface
	istioClient *kube.Client

	// Enable/disable auth, or run both for the tests.
	authmode string

	budget = 90
)

func init() {
	flag.StringVar(&params.infra.Hub, "hub", "gcr.io/istio-testing", "Docker hub")
	flag.StringVar(&params.infra.Tag, "tag", "", "Docker tag")
	flag.StringVar(&params.infra.CaImage, "ca", "gcr.io/istio-testing/istio-ca:"+caTag,
		"CA Docker image")
	flag.StringVar(&params.infra.MixerImage, "mixer", "gcr.io/istio-testing/mixer:"+mixerTag,
		"Mixer Docker image")
	flag.StringVar(&params.infra.Namespace, "n", "",
		"Namespace to use for testing (empty to create/delete temporary one)")
	flag.StringVar(&params.kubeconfig, "kubeconfig", "platform/kube/config",
		"kube config file (missing or empty file makes the test use in-cluster kube config instead)")
	flag.IntVar(&params.count, "count", 1, "Number of times to run the tests after deploying")
	flag.StringVar(&authmode, "auth", "both", "Enable / disable auth, or test both.")
	flag.BoolVar(&params.debug, "debug", false, "Extra logging in the containers")
	flag.BoolVar(&params.logs, "logs", true, "Validate pod logs (expensive in long-running tests)")
}

type test interface {
	String() string
	setup() error
	run() error
	teardown()
}

func main() {
	flag.Parse()
	if params.infra.Tag == "" {
		glog.Fatal("No docker tag specified")
	}

	if params.debug {
		params.infra.Verbosity = 3
	} else {
		params.infra.Verbosity = 2
	}

	check(setupClient())

	params.infra.Mixer = true
	switch authmode {
	case "enable":
		params.infra.Auth = proxyconfig.ProxyMeshConfig_MUTUAL_TLS
		params.infra.Ingress = false
		params.infra.Egress = false
		check(runTests())
	case "disable":
		params.infra.Auth = proxyconfig.ProxyMeshConfig_NONE
		params.infra.Ingress = true
		params.infra.Egress = true
		check(runTests())
	case "both":
		params.infra.Auth = proxyconfig.ProxyMeshConfig_NONE
		params.infra.Ingress = true
		params.infra.Egress = true
		check(runTests())
		params.infra.Auth = proxyconfig.ProxyMeshConfig_MUTUAL_TLS
		params.infra.Ingress = false
		params.infra.Egress = false
		check(runTests())
	default:
		glog.Infof("Invald auth flag: %s. Please choose from: enable/disable/both.", authmode)
	}
}

func check(err error) {
	if err != nil {
		glog.Info(err)
		os.Exit(1)
	}
}

func log(header, s string) {
	glog.Infof("\n\n"+
		"=================== %s =====================\n"+
		"%s\n\n", header, s)
}

func runTests() error {
	istio := params.infra
	log("Deploying infrastructure", spew.Sdump(istio))

	if err := istio.setup(); err != nil {
		return err
	}
	if err := istio.deployApps(); err != nil {
		return err
	}
	var errs error
	istio.apps, errs = util.GetAppPods(client, istio.Namespace)

	tests := []test{
		&reachability{infra: &istio},
		&tcp{infra: &istio},
		&ingress{infra: &istio},
		&egress{infra: &istio},
		&routing{infra: &istio},
	}

	for i := 0; i < params.count; i++ {
		for _, test := range tests {
			log("Setting up test", test.String())
			if err := test.setup(); err != nil {
				errs = multierror.Append(errs, multierror.Prefix(err, test.String()))
			} else {
				log("Running test", test.String())
				if err := test.run(); err != nil {
					errs = multierror.Append(errs, multierror.Prefix(err, test.String()))
				} else {
					log("Success!", test.String())
				}
			}
			log("Tearing down test", test.String())
			test.teardown()
		}
	}

	//  spill proxy logs on error
	if errs != nil {
		for _, pod := range util.GetPods(client, istio.Namespace) {
			log("Proxy log", pod)
			glog.Info(util.FetchLogs(client, pod, istio.Namespace, "proxy"))
		}
	}

	// always remove infra even if the tests fail
	log("Tearing down infrastructure", spew.Sdump(istio))
	istio.teardown()

	if errs == nil {
		log("Passed all tests!", fmt.Sprintf("tests: %v, count: %d", tests, params.count))
	} else {
		log("Failed tests!", errs.Error())
	}
	return errs
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

type status int

const (
	success status = iota
	failure
	again
)

// run in parallel with retries. all funcs must succeed for the function to succeed
func parallel(fs map[string]func() status) error {
	g, ctx := errgroup.WithContext(context.Background())
	repeat := func(name string, f func() status) func() error {
		return func() error {
			for n := 0; n < budget; n++ {
				glog.Infof("%s (attempt %d)", name, n)
				switch f() {
				case failure:
					return fmt.Errorf("Failed %s at attempt %d", name, n)
				case success:
					return nil
				case again:
				}
				select {
				case <-time.After(time.Second):
					// try again
				case <-ctx.Done():
					return nil
				}
			}
			return fmt.Errorf("Failed all %d attempts for %s", budget, name)
		}
	}
	for name, f := range fs {
		g.Go(repeat(name, f))
	}
	return g.Wait()
}

// connect to K8S cluster and register TPRs
func setupClient() error {
	var err error
	istioClient, err = kube.NewClient(params.kubeconfig, model.IstioConfig)
	if err != nil {
		return err
	}
	client = istioClient.GetKubernetesClient()
	return istioClient.RegisterResources()
}
