package john

import (
	"encoding/json"
	"fmt"
	"helm.sh/helm/v3/pkg/strvals"
	"strings"
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

func (m Map) JSON() string {
	b, err := json.Marshal(m)
	if err != nil {
		panic(fmt.Sprintf("json Marshal: %v", err))
	}
	return string(b)
}


func (m Map) MergeInto(other Map) {
	for k, v := range other {
		// Might be a map, possibly recurse
		if v, ok := v.(map[string]any); ok {
			// It's a map...
			if bv, ok := m[k]; ok {
				// And the base map has the same key
				if bv, ok := bv.(map[string]any); ok {
					// And it is a map in the base as well
					Map(bv).MergeInto(v)
					continue
				}
			}
		}
		// Simple overwrite
		m[k] = v
	}
}

// input like `key.subkey=val`
func (m Map) SetPath(path string) error {
	if isAlwaysString(path) {
		return strvals.ParseIntoString(path, m)
	}
	return strvals.ParseInto(path, m)
}

// alwaysString represents types that should always be decoded as strings
// TODO: this could be automatically derived from the value_types.proto?
var alwaysString = []string{
	"values.compatibilityVersion",
	"meshConfig.defaultConfig.proxyMetadata.",
	"compatibilityVersion",
}
func isAlwaysString(s string) bool {
	for _, a := range alwaysString {
		if strings.HasPrefix(s, a) {
			return true
		}
	}
	return false
}