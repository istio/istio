package translate

import (
	"gopkg.in/yaml.v2"

	"istio.io/istio/operator/pkg/util"

	"istio.io/istio/operator/pkg/tpath"
)

// TranslateYAMLTree takes an input tree inTreeStr, a partially constructed output tree outTreeStr, and a map of
// translations of source-path:dest-path in pkg/tpath format. It returns an output tree with paths from the input
// tree, translated and overlaid on the output tree.
func TranslateYAMLTree(inTreeStr, outTreeStr string, translations map[string]string) (string, error) {
	inTree := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(inTreeStr), &inTree); err != nil {
		return "", err
	}
	outTree := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(outTreeStr), &outTree); err != nil {
		return "", err
	}

	for inPath, translation := range translations {
		path := util.PathFromString(inPath)
		node, found, err := tpath.GetFromTreePath(inTree, path)
		if err != nil {
			return "", err
		}
		if !found {
			continue
		}

		if err := tpath.MergeNode(outTree, util.PathFromString(translation), node); err != nil {
			return "", err
		}
	}

	outYAML, err := yaml.Marshal(outTree)
	if err != nil {
		return "", err
	}
	return string(outYAML), nil
}
