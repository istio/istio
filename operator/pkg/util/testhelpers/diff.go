package testhelpers

import (
	"sigs.k8s.io/yaml"

	diff "istio.io/istio/pilot/test/util"
)

// YAMLDiff compares two YAML contents
func YAMLDiff(a, b string) string {
	// Round trip to normalize the format
	roundTrip := func(a string) []byte {
		out := map[string]any{}
		if err := yaml.Unmarshal([]byte(a), &out); err != nil {
			return []byte(a)
		}
		y, err := yaml.Marshal(out)
		if err != nil {
			return []byte(a)
		}
		return y
	}
	err := diff.Compare(roundTrip(a), roundTrip(b))
	if err != nil {
		return err.Error()
	}
	return ""
}
