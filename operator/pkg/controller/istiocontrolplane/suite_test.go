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

package istiocontrolplane

import (
	"fmt"
	"istio.io/istio/operator/pkg/apis"
	"istio.io/istio/operator/pkg/manifest"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var (
	testEnv *envtest.Environment
	cfg *rest.Config
	k8sClient client.Client
	mgr ctrl.Manager
)

const (
	leaderElectionNS = "istio-operator"
	watchNS = "istio-system"
	metricsHost       = "0.0.0.0"
	metricsPort int32 = 8383
)

func TestApis(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecsWithDefaultAndCustomReporters(t,
		"Istio Operator Controller",
		[]Reporter{envtest.NewlineReporter{}})
}

var _ = BeforeSuite(func(done Done) {
	logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(os.Stdout)))
	By("Bootstrapping test environment")
	kubeconfig, err := manifest.InitK8SRestClient("", "")
	Expect(err).ToNot(HaveOccurred())

	t := true
	testEnv = &envtest.Environment{
		UseExistingCluster: &t,
		Config: kubeconfig,
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "deploy", "crds")},
		CRDInstallOptions: envtest.CRDInstallOptions{ErrorIfPathMissing: true},
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	mgr, err := manager.New(cfg, manager.Options{
		Namespace:               watchNS,
		MetricsBindAddress:      fmt.Sprintf("%s:%d", metricsHost, metricsPort),
		LeaderElection:          false,
		LeaderElectionNamespace: leaderElectionNS,
		LeaderElectionID:        "istio-operator-lock",
	})
	Expect(err).ToNot(HaveOccurred())

	err = scheme.AddToScheme(mgr.GetScheme())
	Expect(err).NotTo(HaveOccurred())
	err = apis.AddToScheme(mgr.GetScheme())
	Expect(err).NotTo(HaveOccurred())

	err = Add(mgr)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		err = mgr.Start(ctrl.SetupSignalHandler())
		Expect(err).ToNot(HaveOccurred())
	}()

	k8sClient = mgr.GetClient()
	Expect(k8sClient).ToNot(BeNil())

	close(done)
}, 1000)

var _ = AfterSuite(func() {
	gexec.KillAndWait(5 * time.Second)
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})
