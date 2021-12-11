package v1alpha1

import (
	"encoding/json"

	"github.com/gogo/protobuf/jsonpb"
	github_com_golang_protobuf_jsonpb "github.com/golang/protobuf/jsonpb"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var (
	intv              int64 = 0
	stringv           int64 = 1
	IntOrStringInt          = &intv
	IntOrStringString       = &stringv
)

// UnmarshalJSON implements the json.Unmarshaller interface.
func (this *IntOrString) UnmarshalJSON(value []byte) error {
	if value[0] == '"' {
		this.Type = IntOrStringString
		return json.Unmarshal(value, &this.StrVal)
	}
	this.Type = IntOrStringInt
	return json.Unmarshal(value, &this.IntVal)
}

func (this *IntOrString) MarshalJSONPB(_ *jsonpb.Marshaler) ([]byte, error) {
	return this.MarshalJSON()
}

func (this *IntOrString) MarshalJSON() ([]byte, error) {
	if this.IntVal != nil {
		return json.Marshal(this.IntVal)
	}
	return json.Marshal(this.StrVal)
}

func (this *IntOrString) UnmarshalJSONPB(_ *github_com_golang_protobuf_jsonpb.Unmarshaler, value []byte) error {
	return this.UnmarshalJSON(value)
}

func (this *IntOrString) ToKubernetes() intstr.IntOrString {
	if this.IntVal != nil {
		return intstr.FromInt(int(this.GetIntVal()))
	}
	return intstr.FromString(this.GetStrVal())
}
