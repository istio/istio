MIXERPATH=$GOPATH/src/istio.io/mixer
pushd $MIXERPATH

bazel build template/listentry/... \
template/logentry/... \
template/metric/... \
template/quota/... \
template/reportnothing/... \
template/checknothing/... \

bazel build tools/...

bazel-bin/tools/codegen/cmd/mixgenbootstrap/mixgenbootstrap \
bazel-genfiles/template/listentry/go_default_library_proto.descriptor_set:istio.io/mixer/template/listentry \
bazel-genfiles/template/logentry/go_default_library_proto.descriptor_set:istio.io/mixer/template/logentry \
bazel-genfiles/template/metric/go_default_library_proto.descriptor_set:istio.io/mixer/template/metric \
bazel-genfiles/template/quota/go_default_library_proto.descriptor_set:istio.io/mixer/template/quota \
bazel-genfiles/template/reportnothing/go_default_library_proto.descriptor_set:istio.io/mixer/template/reportnothing \
bazel-genfiles/template/checknothing/go_default_library_proto.descriptor_set:istio.io/mixer/template/checknothing \
-o $MIXERPATH/template/template.gen.go

bazel build ...
