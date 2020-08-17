# Testing the Scripts

## Test install-cni

### Run

```console
$ go test -v install_cni_test.go -args -preconf=... -resultfilename=... -expectedconf=... -expectedclean=...
```

### Description

1. invoke container standalone in raw docker with
   1. test temp dirs mounted to locations for CNI conf/bin & k8s secrets
   1. env vars set like K8s would
   1. mount test script dir
1. Test script
   1. `docker run install-cni` with different env var settings
      1. test conf form format files
         1. with pre-existing .plugins[] (10-calico.conflist)
         1. without pre-existing .plugins[] (minikube_cni.conf)
      1. test cleanup on sigterm
   1. for each case do `cmp -s resultfile expectedfile`
      1. Pass if result file != expected file
