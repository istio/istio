package manifest

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

type Manifest struct {
	*unstructured.Unstructured
	Content string
}

func (m Manifest) Hash() string {
	k := m.GroupVersionKind().Kind
	switch m.GroupVersionKind().Kind {
	case "ClusterRole", "ClusterRoleBinding":
		return k + ":" + m.GetName()
	}
	return k + ":" + m.GetNamespace() + ":" + m.GetName()
}

func FromYaml(y []byte) (Manifest, error) {
	us := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(y, us); err != nil {
		return Manifest{}, err
	}
	return Manifest{
		Unstructured: us,
		Content:      string(y),
	}, nil
}

func FromJson(j []byte) (Manifest, error) {
	us := &unstructured.Unstructured{}
	if err := json.Unmarshal(j, us); err != nil {
		return Manifest{}, err
	}
	yml, err := yaml.Marshal(us)
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{
		Unstructured: us,
		Content:      string(yml),
	}, nil
}

func FromObject(us *unstructured.Unstructured) (Manifest, error) {
	c, err := yaml.Marshal(us)
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{
		Unstructured: us,
		Content:      string(c),
	}, nil
}

func Parse(output []string) ([]Manifest, error) {
	res := make([]Manifest, 0, len(output))
	for _, m := range output {
		mf, err := FromJson([]byte(m))
		if err != nil {
			return nil, err
		}
		if mf.GetObjectKind().GroupVersionKind().Kind == "" {
			// This is not an object. Could be empty template, comments only, etc
			continue
		}
		res = append(res, mf)
	}
	return res, nil
}
