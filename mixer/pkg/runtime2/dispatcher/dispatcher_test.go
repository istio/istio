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

package dispatcher

import (
	"context"
	"testing"

	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/il/compiled"
	"istio.io/istio/mixer/pkg/pool"
	"istio.io/istio/mixer/pkg/runtime2/handler"
	"istio.io/istio/mixer/pkg/runtime2/routing"
	"istio.io/istio/mixer/pkg/runtime2/testing/data"
	"istio.io/istio/mixer/pkg/runtime2/testing/util"
	"istio.io/istio/pkg/log"
)


func TestDispatcher(t *testing.T) {
	o := log.NewOptions()
	o.SetOutputLevel(log.DebugLevel)
	log.Configure(o)

	gp := pool.NewGoroutinePool(10, true)
	dispatcher := New("ident", gp, true)

	templates := data.BuildTemplates()
	adapters := data.BuildAdapters()
	globalConfig := data.JoinConfigs(
		data.HandlerACheck1,
		data.InstanceCheck1,
		data.RuleCheck1,
	)
	s := util.GetSnapshot(templates, adapters, data.ServiceConfig, globalConfig)
	h := handler.NewTable(handler.Empty(), s, gp)

	expb := compiled.NewBuilder(s.Attributes)
	r  := routing.BuildTable(h, s, expb, "istio-system", true)
	_ = dispatcher.ChangeRoute(r)

	bag := attribute.GetFakeMutableBagForTesting(map[string]interface{}{
		"ident": "dest.istio-system",
	})
	_, _ = dispatcher.Check(context.TODO(), bag)
}
