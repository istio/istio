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

package trafficshifting

import (
	"bytes"
	"testing"
	"text/template"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/apps"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/pilot"
)

//	Virtual service topology
//
//						 a
//						|-------|
//						| Host0 |
//						|-------|
//							|
//							|
//							|
//		-------------------------------------
//		|weight1	|weight2	|weight3	|weight4
//		|a			|b			|c			|d
//	|-------|	|-------|	|-------|	|-------|
//	| Host0 |	| Host1	|	| Host2 |	| Host3 |
//	|-------|	|-------|	|-------|	|-------|
//
//

var (
	ist   istio.Instance
	hosts = []string{"a", "b", "c", "d"}
)

const (
	errorBand = 10.0

	virtualService = `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
 name: {{.Name}}
 namespace: {{.Namespace}}
spec:
 hosts:
 - {{.Host0}}
 http:
 - route:
   - destination:
       host: {{.Host0}}
     weight: {{.Weight0}}
   - destination:
       host: {{.Host1}}
     weight: {{.Weight1}}
   - destination:
       host: {{.Host2}}
     weight: {{.Weight2}}
   - destination:
       host: {{.Host3}}
     weight: {{.Weight3}}
`
)

type VirtualServiceConfig struct {
	Name      string
	Host0     string
	Host1     string
	Host2     string
	Host3     string
	Namespace string
	Weight0   int32
	Weight1   int32
	Weight2   int32
	Weight3   int32
}

func TestMain(m *testing.M) {
	framework.Main("traffic_shifting", m, istio.SetupOnKube(&ist, nil))
}

func TestTrafficShifting(t *testing.T) {
	// Traffic distribution
	weights := map[string][]int32{
		"0-100":       {0, 100},
		"20-80":       {20, 80},
		"50-50":       {50, 50},
		"33-33-34":    {33, 33, 34},
		"25-25-25-25": {25, 25, 25, 25},
	}

	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {

			g := galley.NewOrFail(t, ctx, galley.Config{})
			p := pilot.NewOrFail(t, ctx, pilot.Config{Galley: g})

			instance := apps.NewOrFail(t, ctx, apps.Config{Pilot: p, Galley: g})

			for k, v := range weights {
				t.Run(k, func(t *testing.T) {
					v = append(v, make([]int32, 4-len(v))...)

					vsc := VirtualServiceConfig{
						"traffic-shifting-rule",
						hosts[0],
						hosts[1],
						hosts[2],
						hosts[3],
						instance.Namespace().Name(),
						v[0],
						v[1],
						v[2],
						v[3],
					}

					tmpl, _ := template.New("VirtualServiceConfig").Parse(virtualService)
					var buf bytes.Buffer
					tmpl.Execute(&buf, vsc)

					g.ApplyConfigOrFail(t, instance.Namespace(), buf.String())

					// TODO: Find a better way to wait for configuration propagation
					time.Sleep(10 * time.Second)

					from := instance.GetAppOrFail(hosts[0], t).(apps.KubeApp)

					sendTraffic(t, 100, from, hosts[0], hosts, v, errorBand)
				})
			}
		})
}
