package john

import (
	"encoding/json"
	"fmt"
	"strings"

	"helm.sh/helm/v3/pkg/strvals"
	"sigs.k8s.io/yaml"

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

// SetPaths applies values from input like `key.subkey=val`
func (m Map) SetPaths(paths ...string) error {
	for _, path := range paths {
		// Helm supports `foo[0].bar`, but we historically used `foo.[0].bar`
		path := strings.ReplaceAll(path, ".[", "[")
		if isAlwaysString(path) {
			if err := strvals.ParseIntoString(path, m); err != nil {
				return err
			}
		} else {
			if err := strvals.ParseInto(path, m); err != nil {
				return err
			}
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
		cm, ok := asMap(cur)
		if !ok {
			return nil, false
		}
		sub, ok := cm[n]
		if !ok {
			return nil, false
		}
		cur = sub
	}

	return cur, true
}

func asMap(cur any) (Map, bool) {
	if m, ok := cur.(Map); ok {
		return m, true
	}
	if m, ok := cur.(map[string]any); ok {
		return m, true
	}
	return nil, false
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
