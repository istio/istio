package main

import (
	"fmt"
	"gopkg.in/yaml.v2"
	"istio.io/istio/pkg/test/framework/components/cluster"
)

const y = `
- name: Thing
  kind: StaticVM
  controlPlaneCluster: cluster-0
  meta:
    vms:
    - name: a
      ports:
      - protocol: http
        number: 80
`

func main() {
	c := []cluster.Config{}
	if err := yaml.Unmarshal([]byte(y), &c); err != nil {
		panic(err)
	}
	fmt.Println(c)
}
