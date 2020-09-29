module istio.io/istio

go 1.15

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
	cloud.google.com/go v0.65.0
	contrib.go.opencensus.io/exporter/prometheus v0.2.0
	fortio.org/fortio v1.6.8
	github.com/Masterminds/sprig/v3 v3.1.0
	github.com/aws/aws-sdk-go v1.34.20
	github.com/cenkalti/backoff v2.2.1+incompatible
	github.com/census-instrumentation/opencensus-proto v0.3.0
	github.com/cheggaaa/pb/v3 v3.0.5
	github.com/cncf/udpa/go v0.0.0-20200909154343-1f710aca26a9
	github.com/containernetworking/cni v0.7.0-alpha1
	github.com/containernetworking/plugins v0.7.3
	github.com/coreos/go-oidc v2.2.1+incompatible
	github.com/d4l3k/messagediff v1.2.1 // indirect
	github.com/davecgh/go-spew v1.1.1
	github.com/docker/distribution v2.7.1+incompatible
	github.com/envoyproxy/go-control-plane v0.9.7-0.20200916010546-233296ddbe82
	github.com/evanphx/json-patch v4.9.0+incompatible
	github.com/fatih/color v1.9.0
	github.com/fsnotify/fsnotify v1.4.9
	github.com/ghodss/yaml v1.0.0
	github.com/gogo/protobuf v1.3.1
	github.com/golang/protobuf v1.4.2
	github.com/golang/sync v0.0.0-20180314180146-1d60e4601c6f
	github.com/google/go-cmp v0.5.2
	github.com/google/gofuzz v1.2.0
	github.com/google/uuid v1.1.2
	github.com/gorilla/mux v1.8.0
	github.com/gorilla/websocket v1.4.2
	github.com/grpc-ecosystem/go-grpc-middleware v1.2.2
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0
	github.com/hashicorp/go-multierror v1.1.0
	github.com/hashicorp/go-version v1.2.1
	github.com/hashicorp/golang-lru v0.5.4
	github.com/hashicorp/vault/api v1.0.4
	github.com/howeyc/fsnotify v0.9.0
	github.com/kr/pretty v0.2.1
	github.com/kylelemons/godebug v1.1.0
	github.com/lestrrat-go/jwx v1.0.4
	github.com/mattn/go-isatty v0.0.12
	github.com/mholt/archiver/v3 v3.3.0
	github.com/miekg/dns v1.1.31
	github.com/mitchellh/copystructure v1.0.0
	github.com/mitchellh/go-homedir v1.1.0
	github.com/onsi/gomega v1.10.2
	github.com/openshift/api v0.0.0-20200713203337-b2494ecb17dd
	github.com/pkg/errors v0.9.1
	github.com/pmezard/go-difflib v1.0.0
	github.com/prometheus/client_golang v1.7.1
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.10.0
	github.com/ryanuber/go-glob v1.0.0
	github.com/satori/go.uuid v1.2.0
	github.com/soheilhy/cmux v0.1.4
	github.com/spf13/cobra v1.0.0
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.7.1
	github.com/stretchr/testify v1.6.1
	github.com/xeipuuv/gojsonpointer v0.0.0-20190905194746-02993c407bfb // indirect
	github.com/xeipuuv/gojsonreference v0.0.0-20180127040603-bd5ef7bd5415 // indirect
	github.com/yl2chen/cidranger v1.0.1
	go.opencensus.io v0.22.4
	go.uber.org/atomic v1.6.0
	go.uber.org/multierr v1.5.0
	go.uber.org/zap v1.16.0 // indirect
	golang.org/x/crypto v0.0.0-20200820211705-5c72a883971a
	golang.org/x/net v0.0.0-20200904194848-62affa334b73
	golang.org/x/oauth2 v0.0.0-20200902213428-5d25da1a8d43
	golang.org/x/sync v0.0.0-20200625203802-6e8e738ad208
	golang.org/x/time v0.0.0-20200630173020-3af7569d3a1e
	google.golang.org/genproto v0.0.0-20200904004341-0bd0a958aa1d
	google.golang.org/grpc v1.33.0-dev.0.20200828165940-d8ef479ab79a
	google.golang.org/grpc/examples v0.0.0-20200825162801-44d73dff99bf // indirect
	google.golang.org/protobuf v1.25.0
	gopkg.in/d4l3k/messagediff.v1 v1.2.1
	gopkg.in/square/go-jose.v2 v2.5.1
	gopkg.in/yaml.v2 v2.3.0
	gopkg.in/yaml.v3 v3.0.0-20200615113413-eeeca48fe776
	helm.sh/helm/v3 v3.2.4
	istio.io/api v0.0.0-20200929181739-00671adacbea
	istio.io/client-go v0.0.0-20200908160912-f99162621a1a
	istio.io/gogo-genproto v0.0.0-20200908160912-66171252e3db
	istio.io/pkg v0.0.0-20200922180714-670b76a68558
	k8s.io/api v0.19.1
	k8s.io/apiextensions-apiserver v0.19.1
	k8s.io/apimachinery v0.19.1
	k8s.io/cli-runtime v0.19.1
	k8s.io/client-go v0.19.1
	k8s.io/kubectl v0.19.1
	k8s.io/utils v0.0.0-20200821003339-5e75c0163111
	sigs.k8s.io/controller-runtime v0.6.2
	sigs.k8s.io/service-apis v0.0.0-20200916220245-b060b8df63c9
	sigs.k8s.io/yaml v1.2.0
)
