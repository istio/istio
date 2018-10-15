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

package chartutil

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/golang/protobuf/ptypes/timestamp"
	"k8s.io/helm/pkg/proto/hapi/chart"
)

// ErrNoTable indicates that a chart does not have a matching table.
type ErrNoTable error

// ErrNoValue indicates that Values does not contain a key with a value
type ErrNoValue error

// GlobalKey is the name of the Values key that is used for storing global vars.
const GlobalKey = "global"

// Values represents a collection of chart values.
type Values map[string]interface{}

// YAML encodes the Values into a YAML string.
func (v Values) YAML() (string, error) {
	b, err := yaml.Marshal(v)
	return string(b), err
}

// Table gets a table (YAML subsection) from a Values object.
//
// The table is returned as a Values.
//
// Compound table names may be specified with dots:
//
//	foo.bar
//
// The above will be evaluated as "The table bar inside the table
// foo".
//
// An ErrNoTable is returned if the table does not exist.
func (v Values) Table(name string) (Values, error) {
	names := strings.Split(name, ".")
	table := v
	var err error

	for _, n := range names {
		table, err = tableLookup(table, n)
		if err != nil {
			return table, err
		}
	}
	return table, err
}

// AsMap is a utility function for converting Values to a map[string]interface{}.
//
// It protects against nil map panics.
func (v Values) AsMap() map[string]interface{} {
	if v == nil || len(v) == 0 {
		return map[string]interface{}{}
	}
	return v
}

// Encode writes serialized Values information to the given io.Writer.
func (v Values) Encode(w io.Writer) error {
	//return yaml.NewEncoder(w).Encode(v)
	out, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(out)
	return err
}

func tableLookup(v Values, simple string) (Values, error) {
	v2, ok := v[simple]
	if !ok {
		return v, ErrNoTable(fmt.Errorf("no table named %q (%v)", simple, v))
	}
	if vv, ok := v2.(map[string]interface{}); ok {
		return vv, nil
	}

	// This catches a case where a value is of type Values, but doesn't (for some
	// reason) match the map[string]interface{}. This has been observed in the
	// wild, and might be a result of a nil map of type Values.
	if vv, ok := v2.(Values); ok {
		return vv, nil
	}

	var e ErrNoTable = fmt.Errorf("no table named %q", simple)
	return map[string]interface{}{}, e
}

// ReadValues will parse YAML byte data into a Values.
func ReadValues(data []byte) (vals Values, err error) {
	err = yaml.Unmarshal(data, &vals)
	if len(vals) == 0 {
		vals = Values{}
	}
	return
}

// ReadValuesFile will parse a YAML file into a map of values.
func ReadValuesFile(filename string) (Values, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return map[string]interface{}{}, err
	}
	return ReadValues(data)
}

// CoalesceValues coalesces all of the values in a chart (and its subcharts).
//
// Values are coalesced together using the following rules:
//
//	- Values in a higher level chart always override values in a lower-level
//		dependency chart
//	- Scalar values and arrays are replaced, maps are merged
//	- A chart has access to all of the variables for it, as well as all of
//		the values destined for its dependencies.
func CoalesceValues(chrt *chart.Chart, vals *chart.Config) (Values, error) {
	cvals := Values{}
	// Parse values if not nil. We merge these at the top level because
	// the passed-in values are in the same namespace as the parent chart.
	if vals != nil {
		evals, err := ReadValues([]byte(vals.Raw))
		if err != nil {
			return cvals, err
		}
		cvals, err = coalesce(chrt, evals)
		if err != nil {
			return cvals, err
		}
	}

	var err error
	cvals, err = coalesceDeps(chrt, cvals)
	return cvals, err
}

// coalesce coalesces the dest values and the chart values, giving priority to the dest values.
//
// This is a helper function for CoalesceValues.
func coalesce(ch *chart.Chart, dest map[string]interface{}) (map[string]interface{}, error) {
	var err error
	dest, err = coalesceValues(ch, dest)
	if err != nil {
		return dest, err
	}
	coalesceDeps(ch, dest)
	return dest, nil
}

// coalesceDeps coalesces the dependencies of the given chart.
func coalesceDeps(chrt *chart.Chart, dest map[string]interface{}) (map[string]interface{}, error) {
	for _, subchart := range chrt.Dependencies {
		if c, ok := dest[subchart.Metadata.Name]; !ok {
			// If dest doesn't already have the key, create it.
			dest[subchart.Metadata.Name] = map[string]interface{}{}
		} else if !istable(c) {
			return dest, fmt.Errorf("type mismatch on %s: %t", subchart.Metadata.Name, c)
		}
		if dv, ok := dest[subchart.Metadata.Name]; ok {
			dvmap := dv.(map[string]interface{})

			// Get globals out of dest and merge them into dvmap.
			coalesceGlobals(dvmap, dest)

			var err error
			// Now coalesce the rest of the values.
			dest[subchart.Metadata.Name], err = coalesce(subchart, dvmap)
			if err != nil {
				return dest, err
			}
		}
	}
	return dest, nil
}

// coalesceGlobals copies the globals out of src and merges them into dest.
//
// For convenience, returns dest.
func coalesceGlobals(dest, src map[string]interface{}) map[string]interface{} {
	var dg, sg map[string]interface{}

	if destglob, ok := dest[GlobalKey]; !ok {
		dg = map[string]interface{}{}
	} else if dg, ok = destglob.(map[string]interface{}); !ok {
		log.Printf("warning: skipping globals because destination %s is not a table.", GlobalKey)
		return dg
	}

	if srcglob, ok := src[GlobalKey]; !ok {
		sg = map[string]interface{}{}
	} else if sg, ok = srcglob.(map[string]interface{}); !ok {
		log.Printf("warning: skipping globals because source %s is not a table.", GlobalKey)
		return dg
	}

	// EXPERIMENTAL: In the past, we have disallowed globals to test tables. This
	// reverses that decision. It may somehow be possible to introduce a loop
	// here, but I haven't found a way. So for the time being, let's allow
	// tables in globals.
	for key, val := range sg {
		if istable(val) {
			vv := copyMap(val.(map[string]interface{}))
			if destv, ok := dg[key]; ok {
				if destvmap, ok := destv.(map[string]interface{}); ok {
					// Basically, we reverse order of coalesce here to merge
					// top-down.
					coalesceTables(vv, destvmap)
					dg[key] = vv
					continue
				} else {
					log.Printf("Conflict: cannot merge map onto non-map for %q. Skipping.", key)
				}
			} else {
				// Here there is no merge. We're just adding.
				dg[key] = vv
			}
		} else if dv, ok := dg[key]; ok && istable(dv) {
			// It's not clear if this condition can actually ever trigger.
			log.Printf("key %s is table. Skipping", key)
			continue
		}
		// TODO: Do we need to do any additional checking on the value?
		dg[key] = val
	}
	dest[GlobalKey] = dg
	return dest
}

func copyMap(src map[string]interface{}) map[string]interface{} {
	dest := make(map[string]interface{}, len(src))
	for k, v := range src {
		dest[k] = v
	}
	return dest
}

// coalesceValues builds up a values map for a particular chart.
//
// Values in v will override the values in the chart.
func coalesceValues(c *chart.Chart, v map[string]interface{}) (map[string]interface{}, error) {
	// If there are no values in the chart, we just return the given values
	if c.Values == nil || c.Values.Raw == "" {
		return v, nil
	}

	nv, err := ReadValues([]byte(c.Values.Raw))
	if err != nil {
		// On error, we return just the overridden values.
		// FIXME: We should log this error. It indicates that the YAML data
		// did not parse.
		return v, fmt.Errorf("error reading default values (%s): %s", c.Values.Raw, err)
	}

	for key, val := range nv {
		if value, ok := v[key]; ok {
			if value == nil {
				// When the YAML value is null, we remove the value's key.
				// This allows Helm's various sources of values (value files or --set) to
				// remove incompatible keys from any previous chart, file, or set values.
				delete(v, key)
			} else if dest, ok := value.(map[string]interface{}); ok {
				// if v[key] is a table, merge nv's val table into v[key].
				src, ok := val.(map[string]interface{})
				if !ok {
					log.Printf("warning: skipped value for %s: Not a table.", key)
					continue
				}
				// Because v has higher precedence than nv, dest values override src
				// values.
				coalesceTables(dest, src)
			}
		} else {
			// If the key is not in v, copy it from nv.
			v[key] = val
		}
	}
	return v, nil
}

// coalesceTables merges a source map into a destination map.
//
// dest is considered authoritative.
func coalesceTables(dst, src map[string]interface{}) map[string]interface{} {
	// Because dest has higher precedence than src, dest values override src
	// values.
	for key, val := range src {
		if istable(val) {
			if innerdst, ok := dst[key]; !ok {
				dst[key] = val
			} else if istable(innerdst) {
				coalesceTables(innerdst.(map[string]interface{}), val.(map[string]interface{}))
			} else {
				log.Printf("warning: cannot overwrite table with non table for %s (%v)", key, val)
			}
			continue
		} else if dv, ok := dst[key]; ok && istable(dv) {
			log.Printf("warning: destination for %s is a table. Ignoring non-table value %v", key, val)
			continue
		} else if !ok { // <- ok is still in scope from preceding conditional.
			dst[key] = val
			continue
		}
	}
	return dst
}

// ReleaseOptions represents the additional release options needed
// for the composition of the final values struct
type ReleaseOptions struct {
	Name      string
	Time      *timestamp.Timestamp
	Namespace string
	IsUpgrade bool
	IsInstall bool
	Revision  int
}

// ToRenderValues composes the struct from the data coming from the Releases, Charts and Values files
//
// WARNING: This function is deprecated for Helm > 2.1.99 Use ToRenderValuesCaps() instead. It will
// remain in the codebase to stay SemVer compliant.
//
// In Helm 3.0, this will be changed to accept Capabilities as a fourth parameter.
func ToRenderValues(chrt *chart.Chart, chrtVals *chart.Config, options ReleaseOptions) (Values, error) {
	caps := &Capabilities{APIVersions: DefaultVersionSet}
	return ToRenderValuesCaps(chrt, chrtVals, options, caps)
}

// ToRenderValuesCaps composes the struct from the data coming from the Releases, Charts and Values files
//
// This takes both ReleaseOptions and Capabilities to merge into the render values.
func ToRenderValuesCaps(chrt *chart.Chart, chrtVals *chart.Config, options ReleaseOptions, caps *Capabilities) (Values, error) {

	top := map[string]interface{}{
		"Release": map[string]interface{}{
			"Name":      options.Name,
			"Time":      options.Time,
			"Namespace": options.Namespace,
			"IsUpgrade": options.IsUpgrade,
			"IsInstall": options.IsInstall,
			"Revision":  options.Revision,
			"Service":   "Tiller",
		},
		"Chart":        chrt.Metadata,
		"Files":        NewFiles(chrt.Files),
		"Capabilities": caps,
	}

	vals, err := CoalesceValues(chrt, chrtVals)
	if err != nil {
		return top, err
	}

	top["Values"] = vals
	return top, nil
}

// istable is a special-purpose function to see if the present thing matches the definition of a YAML table.
func istable(v interface{}) bool {
	_, ok := v.(map[string]interface{})
	return ok
}

// PathValue takes a path that traverses a YAML structure and returns the value at the end of that path.
// The path starts at the root of the YAML structure and is comprised of YAML keys separated by periods.
// Given the following YAML data the value at path "chapter.one.title" is "Loomings".
//
//	chapter:
//	  one:
//	    title: "Loomings"
func (v Values) PathValue(ypath string) (interface{}, error) {
	if len(ypath) == 0 {
		return nil, errors.New("YAML path string cannot be zero length")
	}
	yps := strings.Split(ypath, ".")
	if len(yps) == 1 {
		// if exists must be root key not table
		vals := v.AsMap()
		k := yps[0]
		if _, ok := vals[k]; ok && !istable(vals[k]) {
			// key found
			return vals[yps[0]], nil
		}
		// key not found
		return nil, ErrNoValue(fmt.Errorf("%v is not a value", k))
	}
	// join all elements of YAML path except last to get string table path
	ypsLen := len(yps)
	table := yps[:ypsLen-1]
	st := strings.Join(table, ".")
	// get the last element as a string key
	key := yps[ypsLen-1:]
	sk := string(key[0])
	// get our table for table path
	t, err := v.Table(st)
	if err != nil {
		//no table
		return nil, ErrNoValue(fmt.Errorf("%v is not a value", sk))
	}
	// check table for key and ensure value is not a table
	if k, ok := t[sk]; ok && !istable(k) {
		// key found
		return k, nil
	}

	// key not found
	return nil, ErrNoValue(fmt.Errorf("key not found: %s", sk))
}
