MIXERPATH=$GOPATH/src/istio.io/mixer
pushd $MIXERPATH

bazel build template/...
bazel build tools/...

bazel-bin/tools/codegen/cmd/mixgenbootstrap/mixgenbootstrap \
bazel-genfiles/template/sample/report/go_default_library_proto.descriptor_set:istio.io/mixer/template/sample/report \
bazel-genfiles/template/sample/check/go_default_library_proto.descriptor_set:istio.io/mixer/template/sample/check \
bazel-genfiles/template/sample/quota/go_default_library_proto.descriptor_set:istio.io/mixer/template/sample/quota \
-o $MIXERPATH/pkg/template/template.gen.go

bazel build ...
