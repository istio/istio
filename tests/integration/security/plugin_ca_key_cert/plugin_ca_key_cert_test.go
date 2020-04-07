// Copyright 2019 Istio Authors
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

package plugincakeycert

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource/environment"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	testNamespace namespace.Instance
	testCtx       framework.TestContext
	testStruct    *testing.T
)

func TestPluginCaKeyCert(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			istioCfg := istio.DefaultConfigOrFail(t, ctx)

			namespace.ClaimOrFail(t, ctx, istioCfg.SystemNamespace)
			testNamespace = namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "test-plugin-ca-key-cert",
				Inject: true,
			})
			testCtx = ctx
			testStruct = t

			// Check that the Istio CA root certificate matches the plugin CA cert.
			retry.UntilSuccessOrFail(t, checkCACert, retry.Delay(time.Second), retry.Timeout(10*time.Second))
		})
}

func checkCACert() error {
	configMapName := "istio-ca-root-cert"
	kEnv := testCtx.Environment().(*kube.Environment)
	cm, err := kEnv.KubeClusters[0].GetConfigMap(configMapName, testNamespace.Name())
	if err != nil {
		return err
	}
	var cert string
	var pluginCert []byte
	var ok bool
	if cert, ok = cm.Data[constants.CACertNamespaceConfigMapDataName]; !ok {
		return fmt.Errorf("CA certificate %v not found", constants.CACertNamespaceConfigMapDataName)
	}
	testStruct.Logf("CA certificate %v found", constants.CACertNamespaceConfigMapDataName)
	if pluginCert, err = readCertFile("root-cert.pem"); err != nil {
		return err
	}
	if string(pluginCert) != cert {
		return fmt.Errorf("CA certificate (%v) not matching plugin cert (%v)", cert, string(pluginCert))
	}

	return nil
}
