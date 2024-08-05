package values

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"sigs.k8s.io/yaml"

	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/pkg/ptr"
)

type Map map[string]any

func MapFromJson(input []byte) (Map, error) {
	m := make(Map)
	err := json.Unmarshal(input, &m)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func MapFromYaml(input []byte) (Map, error) {
	m := make(Map)
	err := yaml.Unmarshal(input, &m)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (m Map) JSON() string {
	b, err := json.Marshal(m)
	if err != nil {
		panic(fmt.Sprintf("json Marshal: %v", err))
	}
	return string(b)
}

func (m Map) YAML() string {
	b, err := yaml.Marshal(m)
	if err != nil {
		panic(fmt.Sprintf("json Marshal: %v", err))
	}
	return string(b)
}

func MakeMap(contents any, path ...string) Map {
	ret := Map{path[len(path)-1]: contents}
	for i := len(path) - 2; i >= 0; i-- {
		ret = Map{path[i]: ret}
	}
	return ret
}

func MakePatch(contents any, in string) string {
	path, err := splitPath(in)
	if err != nil {
		panic("TODO")
	}
	lastSeg := path[len(path)-1]

	var base any
	if k, v, ok := extractKV(lastSeg); ok {
		base = []Map{{"$patch": "delete", k: v}}
	} else {
		base = Map{lastSeg: contents}
	}

	cur := base
	for i := len(path) - 2; i >= 0; i-- {
		seg := path[i]
		if k, v, ok := extractKV(seg); ok {
			cur.(Map)[k] = v
			cur = []any{cur}
		} else if idx, ok := extractIndex(seg); ok {
			panic("!TODO!")
			_ = idx
		} else {
			cur = Map{seg: cur}
		}
	}
	return cur.(Map).JSON()
}

func splitPath(in string) ([]string, error) {
	segments := []string{}
	for {
		if strings.HasPrefix(in, "[") {
			idx := strings.Index(in, "]")
			if idx == -1 {
				return nil, fmt.Errorf("unclosed segment")
			}
			segments = append(segments, in[:idx+1])
			if len(in) <= idx+1 {
				return segments, nil
			}
			in = in[idx+2:]
		} else {
			idx := strings.Index(in, ".")
			if idx == -1 {
				segments = append(segments, in)
				return segments, nil
			}
			segments = append(segments, in[:idx])
			in = in[idx+1:]
		}
	}
}

func extractIndex(seg string) (int, bool) {
	if !strings.HasPrefix(seg, "[") || !strings.HasSuffix(seg, "]") {
		return 0, false
	}
	sanitized := seg[1 : len(seg)-1]
	v, err := strconv.Atoi(sanitized)
	if err != nil {
		return 0, false
	}
	return v, true
}

func extractKV(seg string) (string, string, bool) {
	if !strings.HasPrefix(seg, "[") || !strings.HasSuffix(seg, "]") {
		return "", "", false
	}
	sanitized := seg[1 : len(seg)-1]
	return strings.Cut(sanitized, ":")
}

func (m Map) MergeFrom(other Map) {
	for k, v := range other {
		// Might be a Map or map, possibly recurse
		if vm, ok := v.(Map); ok {
			v = (map[string]any)(vm)
		}
		if v, ok := v.(map[string]any); ok {
			// It's a map...
			if bv, ok := m[k]; ok {
				// And the base map has the same key
				if bv, ok := bv.(map[string]any); ok {
					// And it is a map in the base as well
					Map(bv).MergeFrom(v)
					continue
				}
			}
		}
		// Simple overwrite
		m[k] = v
	}
}

// getPV returns the path and value components for the given set flag string, which must be in path=value format.
func getPV(setFlag string) (path string, value string) {
	pv := strings.Split(setFlag, "=")
	if len(pv) != 2 {
		return setFlag, ""
	}
	path, value = strings.TrimSpace(pv[0]), strings.TrimSpace(pv[1])
	return
}

// SetPaths applies values from input like `key.subkey=val`
func (m Map) SetPaths(paths ...string) error {
	for _, sf := range paths {
		p, v := getPV(sf)
		// input value type is always string, transform it to correct type before setting.
		var val any = v
		if !isAlwaysString(p) {
			val = util.ParseValue(v)
		}
		if err := m.SetPath(p, val); err != nil {
			return err
		}
	}
	return nil
}

// SetPath applies values from a path like `key.subkey`, `key.[0].var`, `key.[name:foo]`
func (m Map) SetPathOld(paths string, value any) error {
	path, err := splitPath(paths)
	if err != nil {
		return err
	}
	var prev any
	var prevKey string
	cur := any(m)
	for _, seg := range path {
		fmt.Println(seg, cur, prev, prevKey)
		if k, v, ok := extractKV(seg); ok {
			l, ok := cur.([]any)
			if l == nil {
				fmt.Println("is nil")
			}
			if !ok {
				return fmt.Errorf("invalid path: %s, data is not a list", seg)
			}
			_ = l
			_ = k
			_ = v
			// cur = []any{cur}
		} else if idx, ok := extractIndex(seg); ok {
			l, ok := cur.([]any)
			if cur == nil {
				// Adding new field
				prev.(Map)[prevKey] = []any{nil}
			} else if !ok {
				// Field exists and is not a list
				return fmt.Errorf("invalid path: %s, data is not a list", seg)
			} else {
				// Modifying existing
				if len(l) <= idx {
					// Index out of bounds, append
					l := append(l, nil)
					prev.(Map)[prevKey] = l
				} else {
					l[idx] = nil
				}
			}
		} else {
			prev = cur
			prevKey = seg
			n, f := cur.(Map)[seg]
			if !f {
				cur.(Map)[seg] = nil
				cur = cur.(Map)[seg]
			} else {
				cur = n
			}
		}
		fmt.Println(cur)
		//inc, _, err := tpath.GetPathContext((map[string]any)(m), util.PathFromString("spec."+p), true)
		//if err != nil {
		//	return err
		//}
		//// input value type is always string, transform it to correct type before setting.
		//var val any = v
		//if !isAlwaysString(p) {
		//	val = util.ParseValue(v)
		//}
		//if err := tpath.WritePathContext(inc, val, false); err != nil {
		//	return err
		//}
	}
	return nil
}

// SetPath applies values from a path like `key.subkey`, `key.[0].var`, `key.[name:foo]`
func (m Map) SetPath(paths string, value any) error {
	path, err := splitPath(paths)
	if err != nil {
		return err
	}
	base := m
	if err := setPathRecurse(base, path, value); err != nil {
		return err
	}
	return nil
}

func setPathRecurse(base map[string]any, paths []string, value any) error {
	seg := paths[0]
	last := len(paths) == 1
	nextIsArray := len(paths) >= 2 && strings.HasPrefix(paths[1], "[")
	if nextIsArray {
		last = len(paths) == 2
		// Find or create target list
		if _, f := base[seg]; !f {
			base[seg] = []any{}
		}
		var index int
		if k, v, ok := extractKV(paths[1]); ok {
			index = -1
			for idx, cm := range base[seg].([]any) {
				if MustAsMap(cm)[k] == v {
					index = idx
					break
				}
			}
			if index == -1 {
				return fmt.Errorf("element %v not found", paths[1])
			}
		} else if idx, ok := extractIndex(paths[1]); ok {
			index = idx
		} else {
			return fmt.Errorf("unknown segment %v", paths[1])
		}
		l := base[seg].([]any)
		if index < 0 || index >= len(l) {
			// Index is greater, we need to append
			if last {
				l = append(l, value)
			} else {
				nm := Map{}
				if err := setPathRecurse(nm, paths[2:], value); err != nil {
					return err
				}
				l = append(l, nm)
			}
			base[seg] = l
		} else {
			v := MustAsMap(l[index])
			if err := setPathRecurse(v, paths[2:], value); err != nil {
				return err
			}
			l[index] = v
		}
	} else {
		// This is a simple key traverse
		// Find or create the target
		// Create if needed
		if _, f := base[seg]; !f {
			base[seg] = map[string]any{}
		}
		if last {
			base[seg] = value
		} else {
			return setPathRecurse(MustAsMap(base[seg]), paths[1:], value)
		}
	}
	return nil
}

// SetSpecPaths applies values from input like `key.subkey=val`, and applies them under 'spec'
func (m Map) SetSpecPaths(paths ...string) error {
	for _, path := range paths {
		if err := m.SetPaths("spec." + path); err != nil {
			return err
		}
	}
	return nil
}

func GetPathAs[T any](m Map, name string) (T, bool) {
	v, ok := m.GetPath(name)
	if !ok {
		return ptr.Empty[T](), false
	}
	t, ok := v.(T)
	return t, ok
}

// TryGetPathAs
func TryGetPathAs[T any](m Map, name string) T {
	v, ok := m.GetPath(name)
	if !ok {
		return ptr.Empty[T]()
	}
	t, ok := v.(T)
	return t
}

// GetPath gets values from input like `key.subkey`
func (m Map) GetPath(name string) (any, bool) {
	cur := any(m)

	for _, n := range parsePath(name) {
		cm, ok := AsMap(cur)
		if !ok {
			return nil, false
		}
		sub, ok := cm[n]
		if !ok {
			return nil, false
		}
		cur = sub
	}

	if p, ok := cur.(*any); ok {
		return *p, true
	}
	return cur, true
}

func AsMap(cur any) (Map, bool) {
	if m, ok := cur.(Map); ok {
		return m, true
	}
	if m, ok := cur.(map[string]any); ok {
		return m, true
	}
	return nil, false
}

func MustAsMap(cur any) Map {
	m, ok := AsMap(cur)
	if !ok {
		panic(fmt.Sprintf("not a map, got %T: %v", cur, cur))
	}
	return m
}

// GetPathMap gets values from input like `key.subkey`
func (m Map) GetPathMap(name string) (Map, bool) {
	cur := m

	for _, n := range parsePath(name) {
		sub, ok := tableLookup(cur, n)
		if !ok {
			return nil, false
		}
		cur = sub
	}
	return cur, true
}

func (m Map) DeepClone() Map {
	// More efficient way?
	res, err := ConvertMap[Map](m)
	if err != nil {
		panic("deep clone should not fail")
	}
	return res
}

func ConvertMap[T any](m Map) (T, error) {
	return FromJson[T]([]byte(m.JSON()))
}

func FromYaml[T any](overlay []byte) (T, error) {
	v := new(T)
	err := yaml.Unmarshal(overlay, &v)
	if err != nil {
		return ptr.Empty[T](), err
	}
	return *v, nil
}

func FromJson[T any](overlay []byte) (T, error) {
	v := new(T)
	err := json.Unmarshal(overlay, &v)
	if err != nil {
		return ptr.Empty[T](), err
	}
	return *v, nil
}

func tableLookup(v Map, simple string) (Map, bool) {
	v2, ok := v[simple]
	if !ok {
		return nil, false
	}
	if vv, ok := v2.(map[string]interface{}); ok {
		return vv, true
	}

	// This catches a case where a value is of type Values, but doesn't (for some
	// reason) match the map[string]interface{}. This has been observed in the
	// wild, and might be a result of a nil map of type Values.
	if vv, ok := v2.(Map); ok {
		return vv, true
	}

	return nil, false
}

func parsePath(key string) []string { return strings.Split(key, ".") }

// alwaysString represents types that should always be decoded as strings
// TODO: this could be automatically derived from the value_types.proto?
var alwaysString = []string{
	"spec.values.compatibilityVersion",
	"spec.meshConfig.defaultConfig.proxyMetadata.",
	"spec.values.meshConfig.defaultConfig.proxyMetadata.",
	"spec.compatibilityVersion",
}

func isAlwaysString(s string) bool {
	for _, a := range alwaysString {
		if strings.HasPrefix(s, a) {
			return true
		}
	}
	return false
}

// GetValueForSetFlag parses the passed set flags which have format key=value and if any set the given path,
// returns the corresponding value, otherwise returns the empty string. setFlags must have valid format.
func GetValueForSetFlag(setFlags []string, path string) string {
	ret := ""
	for _, sf := range setFlags {
		p, v := getPV(sf)
		if p == path {
			ret = v
		}
		// if set multiple times, return last set value
	}
	return ret
}
