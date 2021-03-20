module istio.io/istio

go 1.16

replace github.com/spf13/viper => github.com/istio/viper v1.3.3-0.20190515210538-2789fed3109c

// Old version had no license
replace github.com/chzyer/logex => github.com/chzyer/logex v1.1.11-0.20170329064859-445be9e134b2

// Avoid pulling in incompatible libraries
replace github.com/docker/distribution => github.com/docker/distribution v2.7.1+incompatible

// Avoid pulling in kubernetes/kubernetes
replace github.com/Microsoft/hcsshim => github.com/Microsoft/hcsshim v0.8.8-0.20200421182805-c3e488f0d815

// Client-go does not handle different versions of mergo due to some breaking changes - use the matching version
replace github.com/imdario/mergo => github.com/imdario/mergo v0.3.5

// See https://github.com/kubernetes/kubernetes/issues/92867, there is a bug in the library
replace github.com/evanphx/json-patch => github.com/evanphx/json-patch v0.0.0-20190815234213-e83c0a1c26c8

require (
	cloud.google.com/go v0.78.0
	contrib.go.opencensus.io/exporter/prometheus v0.2.0
	github.com/Masterminds/sprig/v3 v3.2.2
	github.com/aws/aws-sdk-go v1.37.20
	github.com/cenkalti/backoff v2.2.1+incompatible
	github.com/census-instrumentation/opencensus-proto v0.3.0
	github.com/cheggaaa/pb/v3 v3.0.6
	github.com/cncf/udpa/go v0.0.0-20210210032658-bff43e8824d0
	github.com/containernetworking/cni v0.7.0-alpha1
	github.com/containernetworking/plugins v0.7.3
	github.com/coreos/go-oidc v2.2.1+incompatible
	github.com/davecgh/go-spew v1.1.1
	github.com/docker/distribution v2.7.1+incompatible
	github.com/envoyproxy/go-control-plane v0.9.9-0.20210115003313-31f9241a16e6
	github.com/evanphx/json-patch v4.9.0+incompatible
	github.com/evanphx/json-patch/v5 v5.2.0
	github.com/fatih/color v1.10.0
	github.com/fsnotify/fsnotify v1.4.9
	github.com/ghodss/yaml v1.0.0
	github.com/gogo/protobuf v1.3.2
	github.com/golang/protobuf v1.4.3
	github.com/google/go-cmp v0.5.4
	github.com/google/gofuzz v1.2.0
	github.com/google/uuid v1.2.0
	github.com/gorilla/mux v1.8.0
	github.com/gorilla/websocket v1.4.2
	github.com/grpc-ecosystem/go-grpc-middleware v1.2.2
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0
	github.com/hashicorp/go-multierror v1.1.0
	github.com/hashicorp/go-version v1.2.1
	github.com/hashicorp/golang-lru v0.5.4
	github.com/kr/pretty v0.2.1
	github.com/kr/text v0.2.0 // indirect
	github.com/kylelemons/godebug v1.1.0
	github.com/lestrrat-go/jwx v1.1.3
	github.com/lucas-clemente/quic-go v0.19.3
	github.com/mattn/go-isatty v0.0.12
	github.com/mholt/archiver/v3 v3.5.0
	github.com/miekg/dns v1.1.40
	github.com/mitchellh/copystructure v1.1.1
	github.com/mitchellh/go-homedir v1.1.0
	github.com/niemeyer/pretty v0.0.0-20200227124842-a10e7caefd8e // indirect
	github.com/onsi/gomega v1.10.5
	github.com/openshift/api v0.0.0-20200713203337-b2494ecb17dd
	github.com/pkg/errors v0.9.1
	github.com/pmezard/go-difflib v1.0.0
	github.com/prometheus/client_golang v1.9.0
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.18.0
	github.com/ryanuber/go-glob v1.0.0
	github.com/satori/go.uuid v1.2.0
	github.com/soheilhy/cmux v0.1.4
	github.com/spf13/cobra v1.1.3
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.7.1
	github.com/stretchr/testify v1.7.0
	github.com/xeipuuv/gojsonpointer v0.0.0-20190905194746-02993c407bfb // indirect
	github.com/yl2chen/cidranger v1.0.2
	go.opencensus.io v0.23.0
	go.uber.org/atomic v1.7.0
	go.uber.org/multierr v1.6.0
	golang.org/x/crypto v0.0.0-20201221181555-eec23a3978ad // indirect
	golang.org/x/net v0.0.0-20210226172049-e18ecbb05110
	golang.org/x/oauth2 v0.0.0-20210220000619-9bb904979d93
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/sys v0.0.0-20210226181700-f36f78243c0c
	golang.org/x/time v0.0.0-20210220033141-f8bda1e9f3ba
	gomodules.xyz/jsonpatch/v2 v2.1.0
	gomodules.xyz/jsonpatch/v3 v3.0.1
	google.golang.org/genproto v0.0.0-20210222152913-aa3ee6e6a81c
	google.golang.org/grpc v1.36.0
	google.golang.org/grpc/examples v0.0.0-20200825162801-44d73dff99bf // indirect
	google.golang.org/protobuf v1.25.0
	gopkg.in/check.v1 v1.0.0-20200227125254-8fa46927fb4f // indirect
	gopkg.in/square/go-jose.v2 v2.5.1
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b
	helm.sh/helm/v3 v3.4.2
	honnef.co/go/tools v0.0.1-2020.1.5 // indirect
	istio.io/api v0.0.0-20210318104759-fbefbc937cef
	istio.io/client-go v0.0.0-20210218000043-b598dd019200
	istio.io/gogo-genproto v0.0.0-20210226185354-42d28d740a8c
	istio.io/pkg v0.0.0-20210226185257-1f58c1049e9f
	k8s.io/api v0.20.4
	k8s.io/apiextensions-apiserver v0.20.1
	k8s.io/apimachinery v0.20.4
	k8s.io/cli-runtime v0.20.4
	k8s.io/client-go v0.20.4
	k8s.io/kube-openapi v0.0.0-20210216185858-15cd8face8d6
	k8s.io/kubectl v0.20.4
	k8s.io/utils v0.0.0-20210111153108-fddb29f9d009
	sigs.k8s.io/controller-runtime v0.8.2
	sigs.k8s.io/gateway-api v0.2.0
	sigs.k8s.io/mcs-api v0.0.0-20200908023942-d26176718973
	sigs.k8s.io/yaml v1.2.0
)

// Pending https://github.com/kubernetes/kube-openapi/pull/220
replace k8s.io/kube-openapi => github.com/howardjohn/kube-openapi v0.0.0-20210104181841-c0b40d2cb1c8
