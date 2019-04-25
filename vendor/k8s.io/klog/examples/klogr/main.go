package main

import (
	"flag"

	"k8s.io/klog"
	"k8s.io/klog/klogr"
)

type myError struct {
	str string
}

func (e myError) Error() string {
	return e.str
}

func main() {
	flag.Set("v", "3")
	flag.Parse()
	log := klogr.New().WithName("MyName").WithValues("user", "you")
	log.Info("hello", "val1", 1, "val2", map[string]int{"k": 1})
	log.V(3).Info("nice to meet you")
	log.Error(nil, "uh oh", "trouble", true, "reasons", []float64{0.1, 0.11, 3.14})
	log.Error(myError{"an error occurred"}, "goodbye", "code", -1)
	klog.Flush()
}
