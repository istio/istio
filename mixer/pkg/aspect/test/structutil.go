package test

import "github.com/golang/protobuf/ptypes/struct"

// StructMap is shorthand for a map from string to a structpb.Value pointer.
type StructMap map[string]*structpb.Value

// NewStringVal returns a new structpb.Value pointer for a string value.
func NewStringVal(s string) *structpb.Value {
	return &structpb.Value{Kind: &structpb.Value_StringValue{StringValue: s}}
}

// NewStruct returns a new structpb.Struct pointer based on the supplied fields
func NewStruct(fields map[string]*structpb.Value) *structpb.Struct {
	return &structpb.Struct{Fields: fields}
}
