/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package engine

import (
	"bytes"
	"fmt"
	"log"
	"path"
	"sort"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/proto/hapi/chart"
)

// Engine is an implementation of 'cmd/tiller/environment'.Engine that uses Go templates.
type Engine struct {
	// FuncMap contains the template functions that will be passed to each
	// render call. This may only be modified before the first call to Render.
	FuncMap template.FuncMap
	// If strict is enabled, template rendering will fail if a template references
	// a value that was not passed in.
	Strict bool
	// In LintMode, some 'required' template values may be missing, so don't fail
	LintMode bool
}

// New creates a new Go template Engine instance.
//
// The FuncMap is initialized here. You may modify the FuncMap _prior to_ the
// first invocation of Render.
//
// The FuncMap sets all of the Sprig functions except for those that provide
// access to the underlying OS (env, expandenv).
func New() *Engine {
	f := FuncMap()
	return &Engine{
		FuncMap: f,
	}
}

// FuncMap returns a mapping of all of the functions that Engine has.
//
// Because some functions are late-bound (e.g. contain context-sensitive
// data), the functions may not all perform identically outside of an
// Engine as they will inside of an Engine.
//
// Known late-bound functions:
//
//	- "include": This is late-bound in Engine.Render(). The version
//	   included in the FuncMap is a placeholder.
//      - "required": This is late-bound in Engine.Render(). The version
//	   included in the FuncMap is a placeholder.
//      - "tpl": This is late-bound in Engine.Render(). The version
//	   included in the FuncMap is a placeholder.
func FuncMap() template.FuncMap {
	f := sprig.TxtFuncMap()
	delete(f, "env")
	delete(f, "expandenv")

	// Add some extra functionality
	extra := template.FuncMap{
		"toToml":   chartutil.ToToml,
		"toYaml":   chartutil.ToYaml,
		"fromYaml": chartutil.FromYaml,
		"toJson":   chartutil.ToJson,
		"fromJson": chartutil.FromJson,

		// This is a placeholder for the "include" function, which is
		// late-bound to a template. By declaring it here, we preserve the
		// integrity of the linter.
		"include":  func(string, interface{}) string { return "not implemented" },
		"required": func(string, interface{}) interface{} { return "not implemented" },
		"tpl":      func(string, interface{}) interface{} { return "not implemented" },
	}

	for k, v := range extra {
		f[k] = v
	}

	return f
}

// Render takes a chart, optional values, and value overrides, and attempts to render the Go templates.
//
// Render can be called repeatedly on the same engine.
//
// This will look in the chart's 'templates' data (e.g. the 'templates/' directory)
// and attempt to render the templates there using the values passed in.
//
// Values are scoped to their templates. A dependency template will not have
// access to the values set for its parent. If chart "foo" includes chart "bar",
// "bar" will not have access to the values for "foo".
//
// Values should be prepared with something like `chartutils.ReadValues`.
//
// Values are passed through the templates according to scope. If the top layer
// chart includes the chart foo, which includes the chart bar, the values map
// will be examined for a table called "foo". If "foo" is found in vals,
// that section of the values will be passed into the "foo" chart. And if that
// section contains a value named "bar", that value will be passed on to the
// bar chart during render time.
func (e *Engine) Render(chrt *chart.Chart, values chartutil.Values) (map[string]string, error) {
	// Render the charts
	tmap := allTemplates(chrt, values)
	return e.render(tmap)
}

// renderable is an object that can be rendered.
type renderable struct {
	// tpl is the current template.
	tpl string
	// vals are the values to be supplied to the template.
	vals chartutil.Values
	// basePath namespace prefix to the templates of the current chart
	basePath string
}

// alterFuncMap takes the Engine's FuncMap and adds context-specific functions.
//
// The resulting FuncMap is only valid for the passed-in template.
func (e *Engine) alterFuncMap(t *template.Template, referenceTpls map[string]renderable) template.FuncMap {
	// Clone the func map because we are adding context-specific functions.
	var funcMap template.FuncMap = map[string]interface{}{}
	for k, v := range e.FuncMap {
		funcMap[k] = v
	}

	// Add the 'include' function here so we can close over t.
	funcMap["include"] = func(name string, data interface{}) (string, error) {
		buf := bytes.NewBuffer(nil)
		if err := t.ExecuteTemplate(buf, name, data); err != nil {
			return "", err
		}
		return buf.String(), nil
	}

	// Add the 'required' function here
	funcMap["required"] = func(warn string, val interface{}) (interface{}, error) {
		if val == nil {
			if e.LintMode {
				// Don't fail on missing required values when linting
				log.Printf("[INFO] Missing required value: %s", warn)
				return "", nil
			}
			// Convert nil to "" in case required is piped into other functions
			return "", fmt.Errorf(warn)
		} else if _, ok := val.(string); ok {
			if val == "" {
				if e.LintMode {
					// Don't fail on missing required values when linting
					log.Printf("[INFO] Missing required value: %s", warn)
					return val, nil
				}
				return val, fmt.Errorf(warn)
			}
		}
		return val, nil
	}

	// Add the 'tpl' function here
	funcMap["tpl"] = func(tpl string, vals chartutil.Values) (string, error) {
		basePath, err := vals.PathValue("Template.BasePath")
		if err != nil {
			return "", fmt.Errorf("Cannot retrieve Template.Basepath from values inside tpl function: %s (%s)", tpl, err.Error())
		}

		r := renderable{
			tpl:      tpl,
			vals:     vals,
			basePath: basePath.(string),
		}

		templates := map[string]renderable{}
		templateName, err := vals.PathValue("Template.Name")
		if err != nil {
			return "", fmt.Errorf("Cannot retrieve Template.Name from values inside tpl function: %s (%s)", tpl, err.Error())
		}

		templates[templateName.(string)] = r

		result, err := e.renderWithReferences(templates, referenceTpls)
		if err != nil {
			return "", fmt.Errorf("Error during tpl function execution for %q: %s", tpl, err.Error())
		}
		return result[templateName.(string)], nil
	}

	return funcMap
}

// render takes a map of templates/values and renders them.
func (e *Engine) render(tpls map[string]renderable) (rendered map[string]string, err error) {
	return e.renderWithReferences(tpls, tpls)
}

// renderWithReferences takes a map of templates/values to render, and a map of
// templates which can be referenced within them.
func (e *Engine) renderWithReferences(tpls map[string]renderable, referenceTpls map[string]renderable) (rendered map[string]string, err error) {
	// Basically, what we do here is start with an empty parent template and then
	// build up a list of templates -- one for each file. Once all of the templates
	// have been parsed, we loop through again and execute every template.
	//
	// The idea with this process is to make it possible for more complex templates
	// to share common blocks, but to make the entire thing feel like a file-based
	// template engine.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("rendering template failed: %v", r)
		}
	}()
	t := template.New("gotpl")
	if e.Strict {
		t.Option("missingkey=error")
	} else {
		// Not that zero will attempt to add default values for types it knows,
		// but will still emit <no value> for others. We mitigate that later.
		t.Option("missingkey=zero")
	}

	funcMap := e.alterFuncMap(t, referenceTpls)

	// We want to parse the templates in a predictable order. The order favors
	// higher-level (in file system) templates over deeply nested templates.
	keys := sortTemplates(tpls)

	files := []string{}

	for _, fname := range keys {
		r := tpls[fname]
		t = t.New(fname).Funcs(funcMap)
		if _, err := t.Parse(r.tpl); err != nil {
			return map[string]string{}, fmt.Errorf("parse error in %q: %s", fname, err)
		}
		files = append(files, fname)
	}

	// Adding the reference templates to the template context
	// so they can be referenced in the tpl function
	for fname, r := range referenceTpls {
		if t.Lookup(fname) == nil {
			t = t.New(fname).Funcs(funcMap)
			if _, err := t.Parse(r.tpl); err != nil {
				return map[string]string{}, fmt.Errorf("parse error in %q: %s", fname, err)
			}
		}
	}

	rendered = make(map[string]string, len(files))
	var buf bytes.Buffer
	for _, file := range files {
		// Don't render partials. We don't care out the direct output of partials.
		// They are only included from other templates.
		if strings.HasPrefix(path.Base(file), "_") {
			continue
		}
		// At render time, add information about the template that is being rendered.
		vals := tpls[file].vals
		vals["Template"] = map[string]interface{}{"Name": file, "BasePath": tpls[file].basePath}
		if err := t.ExecuteTemplate(&buf, file, vals); err != nil {
			return map[string]string{}, fmt.Errorf("render error in %q: %s", file, err)
		}

		// Work around the issue where Go will emit "<no value>" even if Options(missing=zero)
		// is set. Since missing=error will never get here, we do not need to handle
		// the Strict case.
		rendered[file] = strings.Replace(buf.String(), "<no value>", "", -1)
		buf.Reset()
	}

	return rendered, nil
}

func sortTemplates(tpls map[string]renderable) []string {
	keys := make([]string, len(tpls))
	i := 0
	for key := range tpls {
		keys[i] = key
		i++
	}
	sort.Sort(sort.Reverse(byPathLen(keys)))
	return keys
}

type byPathLen []string

func (p byPathLen) Len() int      { return len(p) }
func (p byPathLen) Swap(i, j int) { p[j], p[i] = p[i], p[j] }
func (p byPathLen) Less(i, j int) bool {
	a, b := p[i], p[j]
	ca, cb := strings.Count(a, "/"), strings.Count(b, "/")
	if ca == cb {
		return strings.Compare(a, b) == -1
	}
	return ca < cb
}

// allTemplates returns all templates for a chart and its dependencies.
//
// As it goes, it also prepares the values in a scope-sensitive manner.
func allTemplates(c *chart.Chart, vals chartutil.Values) map[string]renderable {
	templates := map[string]renderable{}
	recAllTpls(c, templates, vals, true, "")
	return templates
}

// recAllTpls recurses through the templates in a chart.
//
// As it recurses, it also sets the values to be appropriate for the template
// scope.
func recAllTpls(c *chart.Chart, templates map[string]renderable, parentVals chartutil.Values, top bool, parentID string) {
	// This should never evaluate to a nil map. That will cause problems when
	// values are appended later.
	cvals := chartutil.Values{}
	if top {
		// If this is the top of the rendering tree, assume that parentVals
		// is already resolved to the authoritative values.
		cvals = parentVals
	} else if c.Metadata != nil && c.Metadata.Name != "" {
		// If there is a {{.Values.ThisChart}} in the parent metadata,
		// copy that into the {{.Values}} for this template.
		newVals := chartutil.Values{}
		if vs, err := parentVals.Table("Values"); err == nil {
			if tmp, err := vs.Table(c.Metadata.Name); err == nil {
				newVals = tmp
			}
		}

		cvals = map[string]interface{}{
			"Values":       newVals,
			"Release":      parentVals["Release"],
			"Chart":        c.Metadata,
			"Files":        chartutil.NewFiles(c.Files),
			"Capabilities": parentVals["Capabilities"],
		}
	}

	newParentID := c.Metadata.Name
	if parentID != "" {
		// We artificially reconstruct the chart path to child templates. This
		// creates a namespaced filename that can be used to track down the source
		// of a particular template declaration.
		newParentID = path.Join(parentID, "charts", newParentID)
	}

	for _, child := range c.Dependencies {
		recAllTpls(child, templates, cvals, false, newParentID)
	}
	for _, t := range c.Templates {
		templates[path.Join(newParentID, t.Name)] = renderable{
			tpl:      string(t.Data),
			vals:     cvals,
			basePath: path.Join(newParentID, "templates"),
		}
	}
}
