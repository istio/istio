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

package config

import (
	"fmt"
	"reflect"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/config/param"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource/config"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/scopes"
)

// Builder of configuration.
type Builder struct {
	t             framework.TestContext
	out           config.Plan
	needFrom      []Source
	needTo        []Source
	needFromAndTo []Source
	complete      []Source
	yamlCount     int
}

func New(t framework.TestContext) *Builder {
	return NewWithOutput(t, t.ConfigIstio().New())
}

// NewWithOutput creates a new Builder that targets the given config as output.
func NewWithOutput(t framework.TestContext, out config.Plan) *Builder {
	t.Helper()
	checkNotNil("t", t)
	checkNotNil("out", out)

	return &Builder{
		t:   t,
		out: out,
	}
}

func checkNotNil(name string, value interface{}) {
	if value == nil {
		panic(fmt.Sprintf("%s must not be nil", name))
	}
}

// Output returns a copy of this Builder with the given output set.
func (b *Builder) Output(out config.Plan) *Builder {
	b.t.Helper()

	checkNotNil("out", out)

	ret := b.Copy()
	ret.out = out
	return ret
}

// Context returns a copy of this Builder with the given context set.
func (b *Builder) Context(t framework.TestContext) *Builder {
	checkNotNil("t", t)
	out := b.Copy()
	out.t = t
	return out
}

// Source returns a copy of this Builder with the given Source added.
func (b *Builder) Source(s Source) *Builder {
	b.t.Helper()
	out := b.Copy()

	// If the caller set namespaces with literal strings, replace with namespace.Instance objects.
	s = replaceNamespaceStrings(s)

	tpl := s.TemplateOrFail(out.t)
	need := tpl.MissingParams(s.Params())
	needFrom := need.Contains(param.From.String())
	needTo := need.Contains(param.To.String())

	if needFrom && needTo {
		out.needFromAndTo = append(out.needFromAndTo, s)
	} else if needFrom {
		out.needFrom = append(out.needFrom, s)
	} else if needTo {
		out.needTo = append(out.needTo, s)
	} else {
		// No well-known parameters are missing.
		out.complete = append(out.complete, s)
	}

	// Delete all the wellknown parameters.
	need.Delete(param.AllWellKnown().ToStringArray()...)
	if len(need) > 0 {
		panic(fmt.Sprintf("config source missing parameters: %v", need))
	}

	return out
}

// BuildCompleteSources builds only those sources that already contain all parameters
// needed by their templates. Specifically, they are not missing any of the well-known
// parameters: "From", "To", or "Namespace".
func (b *Builder) BuildCompleteSources() *Builder {
	b.t.Helper()
	out := b.Copy()

	systemNS := istio.ClaimSystemNamespaceOrFail(out.t, out.t)

	// Build all the complete config that doesn't require well-known parameters.
	for _, s := range out.complete {
		out.addYAML(withParams(s, param.Params{
			param.SystemNamespace.String(): systemNS,
		}))
	}

	return out
}

// BuildFrom builds only those sources that require only the "From" parameter.
func (b *Builder) BuildFrom(fromAll ...echo.Caller) *Builder {
	b.t.Helper()
	out := b.Copy()

	systemNS := istio.ClaimSystemNamespaceOrFail(out.t, out.t)

	for _, from := range fromAll {
		for _, s := range out.needFrom {
			out.addYAML(withParams(s, param.Params{
				param.From.String():            from,
				param.SystemNamespace.String(): systemNS,
			}))
		}
	}

	return out
}

// BuildFromAndTo builds only those sources that require both the "From" and "To" parameters.
func (b *Builder) BuildFromAndTo(fromAll echo.Callers, toAll echo.Services) *Builder {
	b.t.Helper()
	out := b.Copy()

	systemNS := istio.ClaimSystemNamespaceOrFail(out.t, out.t)

	for _, from := range fromAll {
		for _, to := range toAll {
			for _, s := range out.needFromAndTo {
				out.addYAML(withParams(s, param.Params{
					param.From.String():            from,
					param.To.String():              to,
					param.Namespace.String():       to.Config().Namespace,
					param.SystemNamespace.String(): systemNS,
				}))
			}
		}
	}

	for _, to := range toAll {
		for _, s := range out.needTo {
			out.addYAML(withParams(s, param.Params{
				param.To.String():              to,
				param.Namespace.String():       to.Config().Namespace,
				param.SystemNamespace.String(): systemNS,
			}))
		}
	}

	return out
}

// BuildAll builds the config for all sources.
func (b *Builder) BuildAll(fromAll echo.Callers, toAll echo.Services) *Builder {
	b.t.Helper()

	return b.BuildCompleteSources().
		BuildFrom(fromAll...).
		BuildFromAndTo(fromAll, toAll)
}

func (b *Builder) Apply(opts ...apply.Option) {
	if b.yamlCount == 0 {
		// Nothing to do.
		return
	}

	start := time.Now()
	scopes.Framework.Info("=== BEGIN: Deploy config ===")

	b.out.ApplyOrFail(b.t, opts...)

	scopes.Framework.Infof("=== SUCCEEDED: Deploy config in %v ===", time.Since(start))
}

// Replace any namespace strings with namespace.Instance objects.
func replaceNamespaceStrings(s Source) Source {
	params := s.Params()
	newParams := param.NewParams()
	for _, nsKey := range []param.WellKnown{param.Namespace, param.SystemNamespace} {
		if params.ContainsWellKnown(nsKey) {
			val := params.GetWellKnown(nsKey)
			if strVal, ok := val.(string); ok {
				newParams.SetWellKnown(nsKey, namespace.Static(strVal))
			}
		}
	}
	return s.WithParams(newParams)
}

func (b *Builder) addYAML(s Source) {
	b.t.Helper()

	// Ensure all parameters have been set.
	b.checkMissing(s)

	// Get the namespace where the config should be applied.
	ns := b.getNamespace(s)

	// Generate the YAML and add it to the configuration.
	b.out.YAML(ns.Name(), s.YAMLOrFail(b.t))
	b.yamlCount++
}

func withParams(s Source, params param.Params) Source {
	return s.WithParams(s.Params().SetAllNoOverwrite(params))
}

func (b *Builder) checkMissing(s Source) {
	b.t.Helper()
	tpl := s.TemplateOrFail(b.t)
	missing := tpl.MissingParams(s.Params())
	if missing.Len() > 0 {
		b.t.Fatalf("config template requires missing params: %v", missing.SortedList())
	}
}

func (b *Builder) getNamespace(s Source) namespace.Instance {
	b.t.Helper()
	ns := s.Params().GetWellKnown(param.Namespace)
	if ns == nil {
		b.t.Fatalf("no %s specified in config params", param.Namespace)
	}
	inst, ok := ns.(namespace.Instance)
	if !ok {
		b.t.Fatalf("%s was of unexpected type: %v", param.Namespace, reflect.TypeOf(ns))
	}
	return inst
}

func (b *Builder) Copy() *Builder {
	return &Builder{
		t:             b.t,
		needFrom:      copySources(b.needFrom),
		needTo:        copySources(b.needTo),
		needFromAndTo: copySources(b.needFromAndTo),
		complete:      copySources(b.complete),
		out:           b.out.Copy(),
		yamlCount:     b.yamlCount,
	}
}

func copySources(sources []Source) []Source {
	return append([]Source{}, sources...)
}
