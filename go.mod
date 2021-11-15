module istio.io/istio

go 1.16

replace github.com/spf13/viper => github.com/istio/viper v1.3.3-0.20190515210538-2789fed3109c

// Old version had no license
replace github.com/chzyer/logex => github.com/chzyer/logex v1.1.11-0.20170329064859-445be9e134b2

// Avoid pulling in incompatible libraries
replace github.com/docker/distribution => github.com/docker/distribution v0.0.0-20191216044856-a8371794149d

replace github.com/docker/docker => github.com/moby/moby v17.12.0-ce-rc1.0.20200618181300-9dc6525e6118+incompatible

// Client-go does not handle different versions of mergo due to some breaking changes - use the matching version
replace github.com/imdario/mergo => github.com/imdario/mergo v0.3.5

require (
	cloud.google.com/go v0.97.0
	cloud.google.com/go/security v1.1.0
	contrib.go.opencensus.io/exporter/prometheus v0.4.0
	github.com/AdaLogics/go-fuzz-headers v0.0.0-20210929163055-e81b3f25be97
	github.com/Masterminds/sprig/v3 v3.2.2
	github.com/aws/aws-sdk-go v1.41.7
	github.com/cenkalti/backoff/v4 v4.1.1
	github.com/census-instrumentation/opencensus-proto v0.3.0
	github.com/cheggaaa/pb/v3 v3.0.8
	github.com/cncf/xds/go v0.0.0-20211011173535-cb28da3451f1
	github.com/containernetworking/cni v1.0.1
	github.com/containernetworking/plugins v1.0.1
	github.com/coreos/go-oidc/v3 v3.1.0
	github.com/davecgh/go-spew v1.1.1
	github.com/distribution/distribution/v3 v3.0.0-20210926092439-1563384b69df
	github.com/envoyproxy/go-control-plane v0.9.10-0.20210907150352-cf90f659a021
	github.com/evanphx/json-patch/v5 v5.6.0
	github.com/fatih/color v1.13.0
	github.com/florianl/go-nflog/v2 v2.0.1
	github.com/fsnotify/fsnotify v1.5.1
	github.com/gogo/protobuf v1.3.2
	github.com/golang/protobuf v1.5.2
	github.com/google/go-cmp v0.5.6
	github.com/google/go-containerregistry v0.6.0
	github.com/google/gofuzz v1.2.0
	github.com/google/uuid v1.3.0
	github.com/gorilla/mux v1.8.0
	github.com/gorilla/websocket v1.4.2
	github.com/grpc-ecosystem/go-grpc-middleware v1.3.0
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0
	github.com/hashicorp/go-multierror v1.1.1
	github.com/hashicorp/go-version v1.3.0
	github.com/hashicorp/golang-lru v0.5.4
	github.com/kr/pretty v0.3.0
	github.com/kylelemons/godebug v1.1.0
	github.com/lestrrat-go/jwx v1.2.0
	github.com/lucas-clemente/quic-go v0.24.0
	github.com/mattn/go-isatty v0.0.14
	github.com/miekg/dns v1.1.43
	github.com/mitchellh/copystructure v1.2.0
	github.com/mitchellh/go-homedir v1.1.0
	github.com/onsi/gomega v1.16.0
	github.com/openshift/api v0.0.0-20200713203337-b2494ecb17dd
	github.com/pmezard/go-difflib v1.0.0
	github.com/prometheus/client_golang v1.11.0
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.32.1
	github.com/prometheus/prometheus v2.5.0+incompatible
	github.com/ryanuber/go-glob v1.0.0
	github.com/spf13/cobra v1.2.1
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.8.1
	github.com/stretchr/testify v1.7.0
	github.com/vishvananda/netlink v1.1.1-0.20210330154013-f5de75959ad5
	github.com/xeipuuv/gojsonpointer v0.0.0-20190905194746-02993c407bfb // indirect
	github.com/yl2chen/cidranger v1.0.2
	go.opencensus.io v0.23.0
	go.uber.org/atomic v1.9.0
	go.uber.org/multierr v1.7.0
	golang.org/x/net v0.0.0-20211020060615-d418f374d309
	golang.org/x/oauth2 v0.0.0-20211005180243-6b3c2da341f1
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/sys v0.0.0-20211020174200-9d6173849985
	golang.org/x/time v0.0.0-20210723032227-1f47c861a9ac
	gomodules.xyz/jsonpatch/v2 v2.2.0
	gomodules.xyz/jsonpatch/v3 v3.0.1
	google.golang.org/api v0.59.0
	google.golang.org/genproto v0.0.0-20211020151524-b7c3a969101a
	google.golang.org/grpc v1.42.0
	google.golang.org/protobuf v1.27.1
	gopkg.in/square/go-jose.v2 v2.6.0
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b
	helm.sh/helm/v3 v3.7.1
	istio.io/api v0.0.0-20211115194730-57307006b472
	istio.io/client-go v1.12.0-beta.2.0.20211115195637-e27e6b79344f
	istio.io/gogo-genproto v0.0.0-20211115195057-0e34bdd2be67
	istio.io/pkg v0.0.0-20211115195056-e379f31ee62a
	k8s.io/api v0.22.2
	k8s.io/apiextensions-apiserver v0.22.2
	k8s.io/apimachinery v0.22.2
	k8s.io/cli-runtime v0.22.2
	k8s.io/client-go v0.22.2
	k8s.io/klog/v2 v2.10.0
	k8s.io/kube-openapi v0.0.0-20211020163157-7327e2aaee2b
	k8s.io/kubectl v0.22.2
	k8s.io/utils v0.0.0-20210930125809-cb0fa318a74b
	sigs.k8s.io/controller-runtime v0.10.2
	sigs.k8s.io/gateway-api v0.4.0
	sigs.k8s.io/mcs-api v0.1.0
	sigs.k8s.io/yaml v1.3.0
)
