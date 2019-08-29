### proto file patch instruction
Currently there are two proto files defined temporarily in istio/operator: istiocontrolplane_types.proto and values.types.proto, which would be moved to istio/api after 1.3

There are some type of fields in the original proto files, e.g. []map[string]interface{}, cannot be handled properly by protoc,
so we comment out this fields and patch them back in the generated pb.go files 

#### patch the generated pb.go files
``patch pkg/apis/istio/v1alpha2/istiocontrolplane_types.pb.go < pkg/apis/istio/v1alpha2/fixup_go_structs.patch``

#### generate new patch file
````
# Generate the original *.pb.go file and copy to new file named *.pb.go.orig

# Then Update the pb.go file manually 

# Generate the new patch file

diff -u pkg/apis/istio/v1alpha2/istiocontrolplane_types.pb.go.orig pkg/apis/istio/v1alpha2/istiocontrolplane_types.pb.go > pkg/apis/istio/v1alpha2/fixup_go_structs.patch || true
