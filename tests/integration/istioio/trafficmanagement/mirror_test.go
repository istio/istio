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

package trafficmanagement

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/istioio"
)

// https://preliminary.istio.io/docs/tasks/traffic-management/mirroring/
func TestMirror(t *testing.T) {
	value1 := "ISTIO_IO_MIRROR_TEST_1"
	value2 := "ISTIO_IO_MIRROR_TEST_2"

	framework.
		NewTest(t).
		Run(istioio.NewBuilder("tasks__traffic_management__mirroring").
			Add(createNS()).
			Add(generateHTTPBinDeployment("v1")).
			Add(generateHTTPBinDeployment("v2")).
			Add(generateHTTPBinService()).
			Add(generateSleepDeployment()).
			Add(generateHTTPBinPolicy()).
			Add(istioio.MultiPodWait("istio-io-mirror")).
			Add(sendSomeTraffic(value1, "1")).
			Add(checkLogsV1(value1, "1")).
			Add(checkLogsV2(value1, "1", false)).
			Add(generateMirror(), istioio.SleepCommand(10)).
			Add(sendSomeTraffic(value2, "2")).
			Add(checkLogsV1(value2, "2")).
			Add(checkLogsV2(value2, "2", true)).
			Defer(generateCleanup()).
			Build())
}

func createNS() istioio.Command {
	cmd := istioio.Command{
		Input: istioio.Inline{
			FileName: "create_namespace.sh",
			Value: `
$ kubectl create ns istio-io-mirror
`,
		},
		CreateSnippet:          true,
		IncludeOutputInSnippet: true,
	}

	return cmd
}

func generateCleanup() istioio.Command {
	cmd := istioio.Command{
		Input: istioio.Inline{
			FileName: "delete_namespace.sh",
			Value: `
$ kubectl delete ns istio-io-mirror
`,
		},
		CreateSnippet:          true,
		IncludeOutputInSnippet: true,
	}

	return cmd
}

func sendSomeTraffic(value, suffix string) istioio.Command {
	cmd := istioio.Command{
		Input: istioio.Inline{
			FileName: fmt.Sprintf("generate_traffic_%s.sh", suffix),
			Value: fmt.Sprintf(`
$ export SLEEP_POD=$(kubectl -n istio-io-mirror get pod -l app=sleep -o jsonpath={.items..metadata.name})
$ kubectl -n istio-io-mirror exec ${SLEEP_POD} -c sleep -- curl -o /dev/null -s -w "%%{http_code}\n" http://httpbin:8000/%s
`, value),
		},
		CreateSnippet:          true,
		IncludeOutputInSnippet: true,
	}

	return cmd
}

func checkLogsV1(value, suffix string) istioio.Command {
	cmd := istioio.Command{
		Input: istioio.Inline{
			FileName: fmt.Sprintf("check_logs_v1_%s.sh", suffix),
			Value: `
$ export V1_POD=$(kubectl -n istio-io-mirror get pod -l app=httpbin,version=v1 -o jsonpath={.items..metadata.name})
$ kubectl -n istio-io-mirror logs ${V1_POD} -c httpbin
`,
		},
		Verify:                 istioio.ContainsVerifier(istioio.Inline{Value: value}),
		CreateSnippet:          true,
		IncludeOutputInSnippet: true,
	}

	return cmd
}

func checkLogsV2(value, suffix string, mustContain bool) istioio.Command {
	var verifier istioio.Verifier
	if mustContain {
		verifier = istioio.ContainsVerifier(istioio.Inline{Value: value})
	} else {
		verifier = istioio.NotContainsVerifier(istioio.Inline{Value: value})
	}
	cmd := istioio.Command{
		Input: istioio.Inline{
			FileName: fmt.Sprintf("check_logs_v2_%s.sh", suffix),
			Value: `
$ export V2_POD=$(kubectl -n istio-io-mirror get pod -l app=httpbin,version=v2 -o jsonpath={.items..metadata.name})
$ kubectl -n istio-io-mirror logs ${V2_POD} -c httpbin
`,
		},
		Verify:                 verifier,
		CreateSnippet:          true,
		IncludeOutputInSnippet: true,
	}

	return cmd
}

func generateHTTPBinService() istioio.Command {
	cmd := istioio.Command{
		CreateSnippet: true,
		Input: istioio.Inline{
			FileName: "httpbin_service.sh",
			Value: `
$ kubectl -n istio-io-mirror create -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: httpbin
  labels:
    app: httpbin
spec:
  ports:
  - name: http
    port: 8000
    targetPort: 80
  selector:
    app: httpbin
EOF`,
		},
	}

	return cmd
}

func generateSleepDeployment() istioio.Command {
	cmd := istioio.Command{
		CreateSnippet: true,
		Input: istioio.Inline{
			FileName: "sleep_deployment.sh",
			Value: `
$ cat <<EOF | istioctl kube-inject -f - | kubectl -n istio-io-mirror create -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: sleep
spec:
  replicas: 1
  selector:
    matchLabels:
      app: sleep
  template:
    metadata:
      labels:
        app: sleep
    spec:
      containers:
      - name: sleep
        image: tutum/curl
        command: ["/bin/sleep","infinity"]
        imagePullPolicy: IfNotPresent
EOF`,
		},
	}

	return cmd
}

func generateHTTPBinPolicy() istioio.Command {
	cmd := istioio.Command{
		CreateSnippet: true,
		Input: istioio.Inline{
			FileName: "httpbin_policy.sh",
			Value: `
$ kubectl -n istio-io-mirror apply -f - <<EOF
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: httpbin
spec:
  hosts:
    - httpbin
  http:
  - route:
    - destination:
        host: httpbin
        subset: v1
      weight: 100
---
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: httpbin
spec:
  host: httpbin
  subsets:
  - name: v1
    labels:
      version: v1
  - name: v2
    labels:
      version: v2
EOF`,
		},
	}

	return cmd
}

func generateHTTPBinDeployment(version string) istioio.Command {
	cmd := istioio.Command{
		CreateSnippet: true,
		Input: istioio.Inline{
			FileName: fmt.Sprintf("httpbin_deployment_%s.sh", version),
			Value: fmt.Sprintf(`
$ cat <<EOF | istioctl kube-inject -f - | kubectl -n istio-io-mirror create -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: httpbin-%s
spec:
  replicas: 1
  selector:
    matchLabels:
      app: httpbin
      version: %[1]s
  template:
    metadata:
      labels:
        app: httpbin
        version: %[1]s
    spec:
      containers:
      - image: docker.io/kennethreitz/httpbin
        imagePullPolicy: IfNotPresent
        name: httpbin
        command: ["gunicorn", "--access-logfile", "-", "-b", "0.0.0.0:80", "httpbin:app"]
        ports:
        - containerPort: 80
EOF
`, version),
		},
	}

	return cmd
}

func generateMirror() istioio.Command {
	cmd := istioio.Command{
		CreateSnippet: true,
		Input: istioio.Inline{
			FileName: "mirror_vs.sh",
			Value: `
$ kubectl -n istio-io-mirror apply -f - <<EOF
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: httpbin
spec:
  hosts:
    - httpbin
  http:
  - route:
    - destination:
        host: httpbin
        subset: v1
      weight: 100
    mirror:
      host: httpbin
      subset: v2
    mirror_percent: 100
EOF
`,
		},
	}

	return cmd
}
