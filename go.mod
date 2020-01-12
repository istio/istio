module istio.io/operator

go 1.13

replace k8s.io/klog => github.com/istio/klog v0.0.0-20190424230111-fb7481ea8bcf

replace github.com/golang/glog => github.com/istio/glog v0.0.0-20190424172949-d7cfb6fa2ccd

replace github.com/spf13/viper => github.com/istio/viper v1.3.3-0.20190515210538-2789fed3109c

require (
	github.com/Masterminds/semver v1.4.2 // indirect
	github.com/Masterminds/sprig v2.14.1+incompatible // indirect
	github.com/aokoli/goutils v1.0.1 // indirect
	github.com/cyphar/filepath-securejoin v0.2.2 // indirect
	github.com/docker/distribution v2.7.1+incompatible
	github.com/dsnet/compress v0.0.1 // indirect
	github.com/evanphx/json-patch v4.5.0+incompatible
	github.com/frankban/quicktest v1.4.1 // indirect
	github.com/fsnotify/fsnotify v1.4.7
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v0.1.0
	github.com/go-logr/zapr v0.1.1 // indirect
	github.com/gobwas/glob v0.2.3 // indirect
	github.com/gogo/protobuf v1.3.0
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/golang/protobuf v1.3.2
	github.com/golang/snappy v0.0.1 // indirect
	github.com/google/go-cmp v0.3.1
	github.com/hashicorp/go-version v1.2.0
	github.com/huandu/xstrings v1.2.0 // indirect
	github.com/imdario/mergo v0.3.7 // indirect
	github.com/kr/pretty v0.1.0
	github.com/kylelemons/godebug v1.1.0
	github.com/mholt/archiver v3.1.1+incompatible
	github.com/nwaples/rardecode v1.0.0 // indirect
	github.com/openshift/api v3.9.1-0.20191008181517-e4fd21196097+incompatible // indirect
	github.com/openshift/cluster-network-operator v0.0.0-20191009144453-fdceef8e1a7b
	github.com/pierrec/lz4 v2.2.5+incompatible // indirect
	github.com/pkg/errors v0.8.1
	github.com/prometheus/client_golang v1.1.0 // indirect
	github.com/prometheus/client_model v0.0.0-20190812154241-14fe0d1b01d4 // indirect
	github.com/prometheus/prom2json v1.2.1 // indirect
	github.com/satori/go.uuid v1.2.0 // indirect
	github.com/spf13/cobra v0.0.5
	github.com/stretchr/testify v1.4.0
	github.com/xi2/xz v0.0.0-20171230120015-48954b6210f8 // indirect
	google.golang.org/grpc v1.24.0 // indirect
	gopkg.in/yaml.v2 v2.2.4
	istio.io/api v0.0.0-20200111000814-80fb3f4c4923
	istio.io/pkg v0.0.0-20191029184635-5c2f5ef63692
	k8s.io/api v0.17.0
	k8s.io/apiextensions-apiserver v0.17.0
	k8s.io/apimachinery v0.17.0
	k8s.io/client-go v0.17.0
	k8s.io/helm v2.14.3+incompatible
	k8s.io/kube-proxy v0.17.0 // indirect
	k8s.io/kubectl v0.17.0
	k8s.io/utils v0.0.0-20191114184206-e782cd3c129f
	sigs.k8s.io/controller-runtime v0.4.0
	sigs.k8s.io/yaml v1.1.0
)
