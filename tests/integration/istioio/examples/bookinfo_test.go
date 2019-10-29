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

package security

import (
	"testing"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/istioio"
)

//https://istio.io/docs/examples/bookinfo/
//https://github.com/istio/istio.io/blob/master/content/en/docs/examples/bookinfo/index.md
func TestBookinfo(t *testing.T) {
	framework.
		NewTest(t).
		Run(istioio.NewBuilder("examples__bookinfo").
			Add(istioio.Script{
				Input:   istioio.Path("scripts/bookinfo.txt"),
				WorkDir: env.IstioSrc,
			}).
			Defer(istioio.Script{
				Input: istioio.Inline{
					FileName: "cleanup.sh",
					Value: `
protos=( destinationrules virtualservices gateways )
for proto in "${protos[@]}"; do
  for resource in $(kubectl get -n default "$proto" -o name); do
    kubectl delete -n default "$resource";
  done
done

OUTPUT=$(mktemp)
export OUTPUT
echo "Application cleanup may take up to one minute"
kubectl delete -n default -f samples/bookinfo/platform/kube/bookinfo.yaml > "${OUTPUT}" 2>&1
ret=$?
function cleanup() {
  rm -f "${OUTPUT}"
}

trap cleanup EXIT

if [[ ${ret} -eq 0 ]];then
  cat "${OUTPUT}"
else
  # ignore NotFound errors
  OUT2=$(grep -v NotFound "${OUTPUT}")
  if [[ -n ${OUT2} ]];then
    cat "${OUTPUT}"
    exit ${ret}
  fi
fi

echo "Application cleanup successful"`,
				},
				WorkDir: env.IstioSrc,
			}).
			Build())
}
