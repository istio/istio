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

// This tool simulate envoy sidecar gRPC call to get config,
//
// Usage:
//
// First, you can either manually expose pilot gRPC port, run locally or connect to a real XDS server.
//
// * By port-forward existing pilot - same can be done with Skaffold:
//
// ```bash
// kubectl port-forward $(kubectl get pod -l istio=pilot -o jsonpath={.items[0].metadata.name} -n istio-system) -n istio-system 15010
// ```
//
// * Or run local pilot using the default .kube/config or $KUBECONFIG:
//
//```bash
// pilot-discovery discovery
// ```
//
// To get LDS or CDS, use -type lds or -type cds, and provide the pod labels. For example:
//
// ```bash
// go run pilot_cli.go
// ```
// ```

package main

import (
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	gogojsonpb "github.com/gogo/protobuf/jsonpb"
	golangjsonpb "github.com/golang/protobuf/jsonpb"
	"istio.io/api/mesh/v1alpha1"
	//"io/ioutil"
	//"istio.io/istio/pilot/pkg/model"
	//"istio.io/istio/pkg/adsc"
	istio_agent "istio.io/istio/pkg/istio-agent"
	"istio.io/istio/pkg/security"

	//	"istio.io/istio/pkg/util/gogoprotomarshal"

	_ "net/http"
	_ "net/http/pprof"

	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

var (
	// Same as normal agent
	namespace = env.RegisterStringVar("POD_NAMESPACE", "dev", "Pod namespace")

	// TODO: labels, PROXY_CONFIG

	proxyName  = flag.String("name", "default", "Pod name")
	pilotURL   = flag.String("xds", "localhost:15010", "xds server address.")
	configType = flag.String("typeURL", "", "typeURL or lds, cds, eds. Defaults full config")
	proxyType  = flag.String("proxytype", "sidecar", "sidecar, ingress, router.")
	proxyTag   = flag.String("meta", "", "Pod metadata")
	resources  = flag.String("res", "", "Resource(s) to get config for. LDS/CDS should leave it empty.")
	outputFile = flag.String("out", "", "output file. Leave blank to go to stdout")
)

func main() {
	flag.Parse()

	// Redirect output to file should not include logs
	opt := log.DefaultOptions()
	opt.OutputPaths = []string{"stderr"}
	log.Configure(opt)

	mm := map[string]string{}
	metaS := strings.Split(*proxyTag, ",")
	for _, kv := range metaS {
		kva := strings.Split(kv, "=")
		if len(kva) == 2 {
			mm[kva[0]] = kva[1]
		}
	}
	//watchList := strings.Split(*configType, ",")

	agentc := istio_agent.NewAgent(&v1alpha1.ProxyConfig{
		DiscoveryAddress: *pilotURL,
	}, &istio_agent.AgentConfig{
		LocalXDSAddr: ":9988",
	}, &security.Options{
		RotationInterval: 1 * time.Minute, // required, will refresh the workload cert
	})

	agentc.Start(true, namespace.Get())

	ok := agentc.ADSC.WaitConfigSync(10 * time.Second)
	if !ok {
		log.Warna("Initial config failed, will keep trying ")
	}
	log.Warna("Received ", len(agentc.ADSC.Received))
	//pod := &adsc.Config{
	//	Workload: *proxyName,
	//	Namespace: *namespace,
	//	NodeType: *proxyType,
	//	Watch: watchList,
	//	Meta: model.NodeMetadata {
	//		Namespace: *namespace,
	//		Labels: mm,
	//	}.ToStruct(),
	//}
	//xdsc, err := adsc.New(&v1alpha1.ProxyConfig{
	//	DiscoveryAddress: "127.0.0.1:9988",
	//},  pod)
	//if err != nil {
	//	log.Errorf("Failed to connect Error: %v", err)
	//	return
	//}
	//
	//_, err = xdsc.Wait(10 * time.Second, *configType)
	//if err != nil {
	//	log.Errorf("Failed to get Xds response for %v. Error: %v", *resources, err)
	//	return
	//}
	//
	//resp := xdsc.Received[*configType]
	//
	//// Fails with "unknown message type "envoy.api.v2.Listener"
	//// Envoy is now using golang/protobuf
	//strResponse, err := (&jsonpb.Marshaler{
	//	Indent: "  ",
	//}).MarshalToString(resp)
	//if err != nil {
	//	strResponse, err = gogoprotomarshal.ToJSONWithIndent(resp, " ")
	//	if err != nil {
	//		log.Fatala(err)
	//	}
	//}
	//if outputFile == nil || *outputFile == "" {
	//	fmt.Printf("%v\n", strResponse)
	//} else if err := ioutil.WriteFile(*outputFile, []byte(strResponse), 0644); err != nil {
	//	log.Errorf("Cannot write output to file %q", *outputFile)
	//}

	http.HandleFunc("/dump", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		for t, v := range agentc.ADSC.Received {
			vs, err := (&golangjsonpb.Marshaler{Indent: "  "}).MarshalToString(v)
			if err != nil {
				vs, err = (&gogojsonpb.Marshaler{Indent: "  "}).MarshalToString(v)
				if err != nil {
					vs = err.Error()
				}
			}
			fmt.Fprintf(w, "\"%s\":%v \n", t, vs)
		}
	})
	http.HandleFunc("/sub/", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		err := agentc.ADSC.Send(&discovery.DiscoveryRequest{
			TypeUrl: r.URL.Path[5:]})
		if err != nil {
			log.Warna("Error sending ", err)
		}
	})

	http.ListenAndServe("127.0.0.1:15015", nil)
}
