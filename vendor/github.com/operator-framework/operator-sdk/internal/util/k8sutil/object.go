package k8sutil

import (
	yaml "github.com/ghodss/yaml"
	"k8s.io/apimachinery/pkg/runtime"
)

// GetObjectBytes marshalls an object and removes runtime-managed fields:
// 'status', 'creationTimestamp'
func GetObjectBytes(obj interface{}) ([]byte, error) {
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}
	deleteKeys := []string{"status", "creationTimestamp"}
	for _, dk := range deleteKeys {
		deleteKeyFromUnstructured(u, dk)
	}
	return yaml.Marshal(u)
}

func deleteKeyFromUnstructured(u map[string]interface{}, key string) {
	if _, ok := u[key]; ok {
		delete(u, key)
		return
	}

	for _, v := range u {
		switch t := v.(type) {
		case map[string]interface{}:
			deleteKeyFromUnstructured(t, key)
		case []interface{}:
			for _, ti := range t {
				if m, ok := ti.(map[string]interface{}); ok {
					deleteKeyFromUnstructured(m, key)
				}
			}
		}
	}
}
