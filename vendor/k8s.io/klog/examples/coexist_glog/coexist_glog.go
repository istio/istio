package main

import (
	"flag"

	"github.com/golang/glog"
	"k8s.io/klog"
)

func main() {
	flag.Set("alsologtostderr", "true")
	flag.Parse()

	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)

	// Sync the glog and klog flags.
	flag.CommandLine.VisitAll(func(f1 *flag.Flag) {
		f2 := klogFlags.Lookup(f1.Name)
		if f2 != nil {
			value := f1.Value.String()
			f2.Value.Set(value)
		}
	})

	glog.Info("hello from glog!")
	klog.Info("nice to meet you, I'm klog")
	glog.Flush()
	klog.Flush()
}
