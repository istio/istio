// +build ignore
// This file is omitted when getting with `go get github.com/googleapis/gnostic/...`

package main

import (
	"fmt"
	"sort"

	"github.com/googleapis/gnostic/plugins/gnostic-go-generator/examples/v2.0/apis_guru/apis_guru"
)

func main() {
	c := apis_guru.NewClient("http://api.apis.guru/v2")

	metrics, err := c.GetMetrics()
	if err != nil {
		panic(err)
	}
	fmt.Printf("%+v\n", metrics)

	apis, err := c.ListAPIs()
	if err != nil {
		panic(err)
	}

	keys := make([]string, 0)
	for key, _ := range *apis.OK {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		api := (*apis.OK)[key]
		versions := make([]string, 0)
		for key, _ := range api.Versions {
			versions = append(versions, key)
		}
		sort.Strings(versions)
		fmt.Printf("[%s]:%+v\n", key, versions)
	}

	api := (*apis.OK)["xkcd.com"].Versions["1.0.0"]
	fmt.Printf("%+v\n", api.SwaggerUrl)
}
