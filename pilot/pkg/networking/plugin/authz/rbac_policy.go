// Fake protobuf only used for POC implementation of local RBAC.
// TODO(yangminzhu): Change to use the real protobuf definition in envoy-data-plane.

package authz

import (
	"fmt"
	"math"

	proto "github.com/golang/protobuf/proto"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion2 // please upgrade the proto package

// Should we do white-list or black-list style access control.
type RBAC_Action int32

const (
	// The policies grant access to principals. The rest is denied. This is white-list style
	// access control. This is the default type.
	RBAC_ALLOW RBAC_Action = 0
	// The policies deny access to principals. The rest is allowed. This is black-list style
	// access control.
	RBAC_DENY RBAC_Action = 1
)

var RBAC_Action_name = map[int32]string{
	0: "ALLOW",
	1: "DENY",
}
var RBAC_Action_value = map[string]int32{
	"ALLOW": 0,
	"DENY":  1,
}

func (x RBAC_Action) String() string {
	return proto.EnumName(RBAC_Action_name, int32(x))
}
func (RBAC_Action) EnumDescriptor() ([]byte, []int) { return fileDescriptor0, []int{0, 0} }

type RBAC struct {
	Action RBAC_Action `protobuf:"varint,1,opt,name=action,enum=istio.rbac.v1alpha1.RBAC_Action" json:"action,omitempty"`
	// Maps from policy name to policy.
	Policies map[string]*Policy `protobuf:"bytes,2,rep,name=policies" json:"policies,omitempty" protobuf_key:"bytes,1,opt,name=key" protobuf_val:"bytes,2,opt,name=value"`
}

func (m *RBAC) Reset()                    { *m = RBAC{} }
func (m *RBAC) String() string            { return proto.CompactTextString(m) }
func (*RBAC) ProtoMessage()               {}
func (*RBAC) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{0} }

func (m *RBAC) GetAction() RBAC_Action {
	if m != nil {
		return m.Action
	}
	return RBAC_ALLOW
}

func (m *RBAC) GetPolicies() map[string]*Policy {
	if m != nil {
		return m.Policies
	}
	return nil
}

// Specifies the way to match a string.
type StringMatch struct {
	// Types that are valid to be assigned to MatchPattern:
	//	*StringMatch_Simple
	//	*StringMatch_Prefix
	//	*StringMatch_Suffix
	//	*StringMatch_Regex
	MatchPattern isStringMatch_MatchPattern `protobuf_oneof:"match_pattern"`
}

func (m *StringMatch) Reset()                    { *m = StringMatch{} }
func (m *StringMatch) String() string            { return proto.CompactTextString(m) }
func (*StringMatch) ProtoMessage()               {}
func (*StringMatch) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{1} }

type isStringMatch_MatchPattern interface {
	isStringMatch_MatchPattern()
}

type StringMatch_Simple struct {
	Simple string `protobuf:"bytes,1,opt,name=simple,oneof"`
}
type StringMatch_Prefix struct {
	Prefix string `protobuf:"bytes,2,opt,name=prefix,oneof"`
}
type StringMatch_Suffix struct {
	Suffix string `protobuf:"bytes,3,opt,name=suffix,oneof"`
}
type StringMatch_Regex struct {
	Regex string `protobuf:"bytes,4,opt,name=regex,oneof"`
}

func (*StringMatch_Simple) isStringMatch_MatchPattern() {}
func (*StringMatch_Prefix) isStringMatch_MatchPattern() {}
func (*StringMatch_Suffix) isStringMatch_MatchPattern() {}
func (*StringMatch_Regex) isStringMatch_MatchPattern()  {}

func (m *StringMatch) GetMatchPattern() isStringMatch_MatchPattern {
	if m != nil {
		return m.MatchPattern
	}
	return nil
}

func (m *StringMatch) GetSimple() string {
	if x, ok := m.GetMatchPattern().(*StringMatch_Simple); ok {
		return x.Simple
	}
	return ""
}

func (m *StringMatch) GetPrefix() string {
	if x, ok := m.GetMatchPattern().(*StringMatch_Prefix); ok {
		return x.Prefix
	}
	return ""
}

func (m *StringMatch) GetSuffix() string {
	if x, ok := m.GetMatchPattern().(*StringMatch_Suffix); ok {
		return x.Suffix
	}
	return ""
}

func (m *StringMatch) GetRegex() string {
	if x, ok := m.GetMatchPattern().(*StringMatch_Regex); ok {
		return x.Regex
	}
	return ""
}

// XXX_OneofFuncs is for the internal use of the proto package.
func (*StringMatch) XXX_OneofFuncs() (func(msg proto.Message, b *proto.Buffer) error, func(msg proto.Message, tag, wire int, b *proto.Buffer) (bool, error), func(msg proto.Message) (n int), []interface{}) {
	return _StringMatch_OneofMarshaler, _StringMatch_OneofUnmarshaler, _StringMatch_OneofSizer, []interface{}{
		(*StringMatch_Simple)(nil),
		(*StringMatch_Prefix)(nil),
		(*StringMatch_Suffix)(nil),
		(*StringMatch_Regex)(nil),
	}
}

func _StringMatch_OneofMarshaler(msg proto.Message, b *proto.Buffer) error {
	m := msg.(*StringMatch)
	// match_pattern
	switch x := m.MatchPattern.(type) {
	case *StringMatch_Simple:
		b.EncodeVarint(1<<3 | proto.WireBytes)
		b.EncodeStringBytes(x.Simple)
	case *StringMatch_Prefix:
		b.EncodeVarint(2<<3 | proto.WireBytes)
		b.EncodeStringBytes(x.Prefix)
	case *StringMatch_Suffix:
		b.EncodeVarint(3<<3 | proto.WireBytes)
		b.EncodeStringBytes(x.Suffix)
	case *StringMatch_Regex:
		b.EncodeVarint(4<<3 | proto.WireBytes)
		b.EncodeStringBytes(x.Regex)
	case nil:
	default:
		return fmt.Errorf("StringMatch.MatchPattern has unexpected type %T", x)
	}
	return nil
}

func _StringMatch_OneofUnmarshaler(msg proto.Message, tag, wire int, b *proto.Buffer) (bool, error) {
	m := msg.(*StringMatch)
	switch tag {
	case 1: // match_pattern.simple
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		x, err := b.DecodeStringBytes()
		m.MatchPattern = &StringMatch_Simple{x}
		return true, err
	case 2: // match_pattern.prefix
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		x, err := b.DecodeStringBytes()
		m.MatchPattern = &StringMatch_Prefix{x}
		return true, err
	case 3: // match_pattern.suffix
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		x, err := b.DecodeStringBytes()
		m.MatchPattern = &StringMatch_Suffix{x}
		return true, err
	case 4: // match_pattern.regex
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		x, err := b.DecodeStringBytes()
		m.MatchPattern = &StringMatch_Regex{x}
		return true, err
	default:
		return false, nil
	}
}

func _StringMatch_OneofSizer(msg proto.Message) (n int) {
	m := msg.(*StringMatch)
	// match_pattern
	switch x := m.MatchPattern.(type) {
	case *StringMatch_Simple:
		n += proto.SizeVarint(1<<3 | proto.WireBytes)
		n += proto.SizeVarint(uint64(len(x.Simple)))
		n += len(x.Simple)
	case *StringMatch_Prefix:
		n += proto.SizeVarint(2<<3 | proto.WireBytes)
		n += proto.SizeVarint(uint64(len(x.Prefix)))
		n += len(x.Prefix)
	case *StringMatch_Suffix:
		n += proto.SizeVarint(3<<3 | proto.WireBytes)
		n += proto.SizeVarint(uint64(len(x.Suffix)))
		n += len(x.Suffix)
	case *StringMatch_Regex:
		n += proto.SizeVarint(4<<3 | proto.WireBytes)
		n += proto.SizeVarint(uint64(len(x.Regex)))
		n += len(x.Regex)
	case nil:
	default:
		panic(fmt.Sprintf("proto: unexpected type %T in oneof", x))
	}
	return n
}

// Policy specifies a role and the principals that are assigned/denied the role.
type Policy struct {
	// Required. The set of permissions that define a role.
	Permissions []*Permission `protobuf:"bytes,1,rep,name=permissions" json:"permissions,omitempty"`
	// Required. List of principals that are assigned/denied the role based on “action”.
	Principals []*Principal `protobuf:"bytes,2,rep,name=principals" json:"principals,omitempty"`
}

func (m *Policy) Reset()                    { *m = Policy{} }
func (m *Policy) String() string            { return proto.CompactTextString(m) }
func (*Policy) ProtoMessage()               {}
func (*Policy) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{2} }

func (m *Policy) GetPermissions() []*Permission {
	if m != nil {
		return m.Permissions
	}
	return nil
}

func (m *Policy) GetPrincipals() []*Principal {
	if m != nil {
		return m.Principals
	}
	return nil
}

// Specifies how to match an entry in a map.
type MapEntryMatch struct {
	// The key to select an entry from the map.
	Key string `protobuf:"bytes,1,opt,name=key" json:"key,omitempty"`
	// A list of matched values.
	Values []*StringMatch `protobuf:"bytes,2,rep,name=values" json:"values,omitempty"`
}

func (m *MapEntryMatch) Reset()                    { *m = MapEntryMatch{} }
func (m *MapEntryMatch) String() string            { return proto.CompactTextString(m) }
func (*MapEntryMatch) ProtoMessage()               {}
func (*MapEntryMatch) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{3} }

func (m *MapEntryMatch) GetKey() string {
	if m != nil {
		return m.Key
	}
	return ""
}

func (m *MapEntryMatch) GetValues() []*StringMatch {
	if m != nil {
		return m.Values
	}
	return nil
}

// CidrRange specifies an IP Address and a prefix length to construct
// the subnet mask for a `CIDR <https://tools.ietf.org/html/rfc4632>`_ range.
type CidrRange struct {
	// IPv4 or IPv6 address, e.g. ``192.0.0.0`` or ``2001:db8::``.
	AddressPrefix string `protobuf:"bytes,1,opt,name=address_prefix,json=addressPrefix" json:"address_prefix,omitempty"`
	// Length of prefix, e.g. 0, 32.
	PrefixLen uint32 `protobuf:"varint,2,opt,name=prefix_len,json=prefixLen" json:"prefix_len,omitempty"`
}

func (m *CidrRange) Reset()                    { *m = CidrRange{} }
func (m *CidrRange) String() string            { return proto.CompactTextString(m) }
func (*CidrRange) ProtoMessage()               {}
func (*CidrRange) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{4} }

func (m *CidrRange) GetAddressPrefix() string {
	if m != nil {
		return m.AddressPrefix
	}
	return ""
}

func (m *CidrRange) GetPrefixLen() uint32 {
	if m != nil {
		return m.PrefixLen
	}
	return 0
}

// Specifies how to match IP addresses.
type IpMatch struct {
	// IP addresses in CIDR notation.
	Cidrs []*CidrRange `protobuf:"bytes,1,rep,name=cidrs" json:"cidrs,omitempty"`
}

func (m *IpMatch) Reset()                    { *m = IpMatch{} }
func (m *IpMatch) String() string            { return proto.CompactTextString(m) }
func (*IpMatch) ProtoMessage()               {}
func (*IpMatch) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{5} }

func (m *IpMatch) GetCidrs() []*CidrRange {
	if m != nil {
		return m.Cidrs
	}
	return nil
}

// Specifies how to match ports.
type PortMatch struct {
	// Port numbers.
	Ports []uint32 `protobuf:"varint,1,rep,packed,name=ports" json:"ports,omitempty"`
}

func (m *PortMatch) Reset()                    { *m = PortMatch{} }
func (m *PortMatch) String() string            { return proto.CompactTextString(m) }
func (*PortMatch) ProtoMessage()               {}
func (*PortMatch) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{6} }

func (m *PortMatch) GetPorts() []uint32 {
	if m != nil {
		return m.Ports
	}
	return nil
}

// Permission defines a permission to access the service.
type Permission struct {
	// Optional. A list of HTTP paths or gRPC methods.
	// gRPC methods must be presented as fully-qualified name in the form of
	// packageName.serviceName/methodName.
	// If this field is unset, it applies to any path.
	Paths []*StringMatch `protobuf:"bytes,1,rep,name=paths" json:"paths,omitempty"`
	// Required. A list of HTTP methods (e.g., "GET", "POST").
	// If this field is unset, it applies to any method.
	Methods []string `protobuf:"bytes,2,rep,name=methods" json:"methods,omitempty"`
	// Optional. Custom conditions.
	Conditions []*Permission_Condition `protobuf:"bytes,3,rep,name=conditions" json:"conditions,omitempty"`
}

func (m *Permission) Reset()                    { *m = Permission{} }
func (m *Permission) String() string            { return proto.CompactTextString(m) }
func (*Permission) ProtoMessage()               {}
func (*Permission) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{7} }

func (m *Permission) GetPaths() []*StringMatch {
	if m != nil {
		return m.Paths
	}
	return nil
}

func (m *Permission) GetMethods() []string {
	if m != nil {
		return m.Methods
	}
	return nil
}

func (m *Permission) GetConditions() []*Permission_Condition {
	if m != nil {
		return m.Conditions
	}
	return nil
}

// Definition of a custom condition.
type Permission_Condition struct {
	// Types that are valid to be assigned to ConditionSpec:
	//	*Permission_Condition_Header
	//	*Permission_Condition_DestinationIps
	//	*Permission_Condition_DestinationPorts
	ConditionSpec isPermission_Condition_ConditionSpec `protobuf_oneof:"condition_spec"`
}

func (m *Permission_Condition) Reset()                    { *m = Permission_Condition{} }
func (m *Permission_Condition) String() string            { return proto.CompactTextString(m) }
func (*Permission_Condition) ProtoMessage()               {}
func (*Permission_Condition) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{7, 0} }

type isPermission_Condition_ConditionSpec interface {
	isPermission_Condition_ConditionSpec()
}

type Permission_Condition_Header struct {
	Header *MapEntryMatch `protobuf:"bytes,1,opt,name=header,oneof"`
}
type Permission_Condition_DestinationIps struct {
	DestinationIps *IpMatch `protobuf:"bytes,2,opt,name=destination_ips,json=destinationIps,oneof"`
}
type Permission_Condition_DestinationPorts struct {
	DestinationPorts *PortMatch `protobuf:"bytes,3,opt,name=destination_ports,json=destinationPorts,oneof"`
}

func (*Permission_Condition_Header) isPermission_Condition_ConditionSpec()           {}
func (*Permission_Condition_DestinationIps) isPermission_Condition_ConditionSpec()   {}
func (*Permission_Condition_DestinationPorts) isPermission_Condition_ConditionSpec() {}

func (m *Permission_Condition) GetConditionSpec() isPermission_Condition_ConditionSpec {
	if m != nil {
		return m.ConditionSpec
	}
	return nil
}

func (m *Permission_Condition) GetHeader() *MapEntryMatch {
	if x, ok := m.GetConditionSpec().(*Permission_Condition_Header); ok {
		return x.Header
	}
	return nil
}

func (m *Permission_Condition) GetDestinationIps() *IpMatch {
	if x, ok := m.GetConditionSpec().(*Permission_Condition_DestinationIps); ok {
		return x.DestinationIps
	}
	return nil
}

func (m *Permission_Condition) GetDestinationPorts() *PortMatch {
	if x, ok := m.GetConditionSpec().(*Permission_Condition_DestinationPorts); ok {
		return x.DestinationPorts
	}
	return nil
}

// XXX_OneofFuncs is for the internal use of the proto package.
func (*Permission_Condition) XXX_OneofFuncs() (func(msg proto.Message, b *proto.Buffer) error, func(msg proto.Message, tag, wire int, b *proto.Buffer) (bool, error), func(msg proto.Message) (n int), []interface{}) {
	return _Permission_Condition_OneofMarshaler, _Permission_Condition_OneofUnmarshaler, _Permission_Condition_OneofSizer, []interface{}{
		(*Permission_Condition_Header)(nil),
		(*Permission_Condition_DestinationIps)(nil),
		(*Permission_Condition_DestinationPorts)(nil),
	}
}

func _Permission_Condition_OneofMarshaler(msg proto.Message, b *proto.Buffer) error {
	m := msg.(*Permission_Condition)
	// condition_spec
	switch x := m.ConditionSpec.(type) {
	case *Permission_Condition_Header:
		b.EncodeVarint(1<<3 | proto.WireBytes)
		if err := b.EncodeMessage(x.Header); err != nil {
			return err
		}
	case *Permission_Condition_DestinationIps:
		b.EncodeVarint(2<<3 | proto.WireBytes)
		if err := b.EncodeMessage(x.DestinationIps); err != nil {
			return err
		}
	case *Permission_Condition_DestinationPorts:
		b.EncodeVarint(3<<3 | proto.WireBytes)
		if err := b.EncodeMessage(x.DestinationPorts); err != nil {
			return err
		}
	case nil:
	default:
		return fmt.Errorf("Permission_Condition.ConditionSpec has unexpected type %T", x)
	}
	return nil
}

func _Permission_Condition_OneofUnmarshaler(msg proto.Message, tag, wire int, b *proto.Buffer) (bool, error) {
	m := msg.(*Permission_Condition)
	switch tag {
	case 1: // condition_spec.header
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		msg := new(MapEntryMatch)
		err := b.DecodeMessage(msg)
		m.ConditionSpec = &Permission_Condition_Header{msg}
		return true, err
	case 2: // condition_spec.destination_ips
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		msg := new(IpMatch)
		err := b.DecodeMessage(msg)
		m.ConditionSpec = &Permission_Condition_DestinationIps{msg}
		return true, err
	case 3: // condition_spec.destination_ports
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		msg := new(PortMatch)
		err := b.DecodeMessage(msg)
		m.ConditionSpec = &Permission_Condition_DestinationPorts{msg}
		return true, err
	default:
		return false, nil
	}
}

func _Permission_Condition_OneofSizer(msg proto.Message) (n int) {
	m := msg.(*Permission_Condition)
	// condition_spec
	switch x := m.ConditionSpec.(type) {
	case *Permission_Condition_Header:
		s := proto.Size(x.Header)
		n += proto.SizeVarint(1<<3 | proto.WireBytes)
		n += proto.SizeVarint(uint64(s))
		n += s
	case *Permission_Condition_DestinationIps:
		s := proto.Size(x.DestinationIps)
		n += proto.SizeVarint(2<<3 | proto.WireBytes)
		n += proto.SizeVarint(uint64(s))
		n += s
	case *Permission_Condition_DestinationPorts:
		s := proto.Size(x.DestinationPorts)
		n += proto.SizeVarint(3<<3 | proto.WireBytes)
		n += proto.SizeVarint(uint64(s))
		n += s
	case nil:
	default:
		panic(fmt.Sprintf("proto: unexpected type %T in oneof", x))
	}
	return n
}

// Principal defines an identity or a group of identities.
type Principal struct {
	// Optional. Authenticated attributes that identify the principal.
	Authenticated *Principal_Authenticated `protobuf:"bytes,1,opt,name=authenticated" json:"authenticated,omitempty"`
	// Optional. Custom attributes that identify the principal.
	Attributes []*Principal_Attribute `protobuf:"bytes,2,rep,name=attributes" json:"attributes,omitempty"`
}

func (m *Principal) Reset()                    { *m = Principal{} }
func (m *Principal) String() string            { return proto.CompactTextString(m) }
func (*Principal) ProtoMessage()               {}
func (*Principal) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{8} }

func (m *Principal) GetAuthenticated() *Principal_Authenticated {
	if m != nil {
		return m.Authenticated
	}
	return nil
}

func (m *Principal) GetAttributes() []*Principal_Attribute {
	if m != nil {
		return m.Attributes
	}
	return nil
}

// Authentication attributes for principal. These could be filled out inside RBAC filter.
// Or if an authentication filter is used, they can be provided by the authentication filter.
type Principal_Authenticated struct {
	// Optional. The name of the principal. This matches to the "source.principal" field in
	// ":ref: `AttributeContext <envoy_api_msg_service.auth.v2alpha.AttributeContext>`.
	// If unset, it applies to any user.
	Name string `protobuf:"bytes,1,opt,name=name" json:"name,omitempty"`
}

func (m *Principal_Authenticated) Reset()                    { *m = Principal_Authenticated{} }
func (m *Principal_Authenticated) String() string            { return proto.CompactTextString(m) }
func (*Principal_Authenticated) ProtoMessage()               {}
func (*Principal_Authenticated) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{8, 0} }

func (m *Principal_Authenticated) GetName() string {
	if m != nil {
		return m.Name
	}
	return ""
}

// Definition of a custom attribute to identify the principal.
type Principal_Attribute struct {
	// Types that are valid to be assigned to AttributeSpec:
	//	*Principal_Attribute_Service
	//	*Principal_Attribute_SourceIps
	//	*Principal_Attribute_Header
	AttributeSpec isPrincipal_Attribute_AttributeSpec `protobuf_oneof:"attribute_spec"`
}

func (m *Principal_Attribute) Reset()                    { *m = Principal_Attribute{} }
func (m *Principal_Attribute) String() string            { return proto.CompactTextString(m) }
func (*Principal_Attribute) ProtoMessage()               {}
func (*Principal_Attribute) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{8, 1} }

type isPrincipal_Attribute_AttributeSpec interface {
	isPrincipal_Attribute_AttributeSpec()
}

type Principal_Attribute_Service struct {
	Service string `protobuf:"bytes,1,opt,name=service,oneof"`
}
type Principal_Attribute_SourceIps struct {
	SourceIps *IpMatch `protobuf:"bytes,2,opt,name=source_ips,json=sourceIps,oneof"`
}
type Principal_Attribute_Header struct {
	Header *MapEntryMatch `protobuf:"bytes,3,opt,name=header,oneof"`
}

func (*Principal_Attribute_Service) isPrincipal_Attribute_AttributeSpec()   {}
func (*Principal_Attribute_SourceIps) isPrincipal_Attribute_AttributeSpec() {}
func (*Principal_Attribute_Header) isPrincipal_Attribute_AttributeSpec()    {}

func (m *Principal_Attribute) GetAttributeSpec() isPrincipal_Attribute_AttributeSpec {
	if m != nil {
		return m.AttributeSpec
	}
	return nil
}

func (m *Principal_Attribute) GetService() string {
	if x, ok := m.GetAttributeSpec().(*Principal_Attribute_Service); ok {
		return x.Service
	}
	return ""
}

func (m *Principal_Attribute) GetSourceIps() *IpMatch {
	if x, ok := m.GetAttributeSpec().(*Principal_Attribute_SourceIps); ok {
		return x.SourceIps
	}
	return nil
}

func (m *Principal_Attribute) GetHeader() *MapEntryMatch {
	if x, ok := m.GetAttributeSpec().(*Principal_Attribute_Header); ok {
		return x.Header
	}
	return nil
}

// XXX_OneofFuncs is for the internal use of the proto package.
func (*Principal_Attribute) XXX_OneofFuncs() (func(msg proto.Message, b *proto.Buffer) error, func(msg proto.Message, tag, wire int, b *proto.Buffer) (bool, error), func(msg proto.Message) (n int), []interface{}) {
	return _Principal_Attribute_OneofMarshaler, _Principal_Attribute_OneofUnmarshaler, _Principal_Attribute_OneofSizer, []interface{}{
		(*Principal_Attribute_Service)(nil),
		(*Principal_Attribute_SourceIps)(nil),
		(*Principal_Attribute_Header)(nil),
	}
}

func _Principal_Attribute_OneofMarshaler(msg proto.Message, b *proto.Buffer) error {
	m := msg.(*Principal_Attribute)
	// attribute_spec
	switch x := m.AttributeSpec.(type) {
	case *Principal_Attribute_Service:
		b.EncodeVarint(1<<3 | proto.WireBytes)
		b.EncodeStringBytes(x.Service)
	case *Principal_Attribute_SourceIps:
		b.EncodeVarint(2<<3 | proto.WireBytes)
		if err := b.EncodeMessage(x.SourceIps); err != nil {
			return err
		}
	case *Principal_Attribute_Header:
		b.EncodeVarint(3<<3 | proto.WireBytes)
		if err := b.EncodeMessage(x.Header); err != nil {
			return err
		}
	case nil:
	default:
		return fmt.Errorf("Principal_Attribute.AttributeSpec has unexpected type %T", x)
	}
	return nil
}

func _Principal_Attribute_OneofUnmarshaler(msg proto.Message, tag, wire int, b *proto.Buffer) (bool, error) {
	m := msg.(*Principal_Attribute)
	switch tag {
	case 1: // attribute_spec.service
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		x, err := b.DecodeStringBytes()
		m.AttributeSpec = &Principal_Attribute_Service{x}
		return true, err
	case 2: // attribute_spec.source_ips
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		msg := new(IpMatch)
		err := b.DecodeMessage(msg)
		m.AttributeSpec = &Principal_Attribute_SourceIps{msg}
		return true, err
	case 3: // attribute_spec.header
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		msg := new(MapEntryMatch)
		err := b.DecodeMessage(msg)
		m.AttributeSpec = &Principal_Attribute_Header{msg}
		return true, err
	default:
		return false, nil
	}
}

func _Principal_Attribute_OneofSizer(msg proto.Message) (n int) {
	m := msg.(*Principal_Attribute)
	// attribute_spec
	switch x := m.AttributeSpec.(type) {
	case *Principal_Attribute_Service:
		n += proto.SizeVarint(1<<3 | proto.WireBytes)
		n += proto.SizeVarint(uint64(len(x.Service)))
		n += len(x.Service)
	case *Principal_Attribute_SourceIps:
		s := proto.Size(x.SourceIps)
		n += proto.SizeVarint(2<<3 | proto.WireBytes)
		n += proto.SizeVarint(uint64(s))
		n += s
	case *Principal_Attribute_Header:
		s := proto.Size(x.Header)
		n += proto.SizeVarint(3<<3 | proto.WireBytes)
		n += proto.SizeVarint(uint64(s))
		n += s
	case nil:
	default:
		panic(fmt.Sprintf("proto: unexpected type %T in oneof", x))
	}
	return n
}

func init() {
	proto.RegisterType((*RBAC)(nil), "istio.rbac.v1alpha1.RBAC")
	proto.RegisterType((*StringMatch)(nil), "istio.rbac.v1alpha1.StringMatch")
	proto.RegisterType((*Policy)(nil), "istio.rbac.v1alpha1.Policy")
	proto.RegisterType((*MapEntryMatch)(nil), "istio.rbac.v1alpha1.MapEntryMatch")
	proto.RegisterType((*CidrRange)(nil), "istio.rbac.v1alpha1.CidrRange")
	proto.RegisterType((*IpMatch)(nil), "istio.rbac.v1alpha1.IpMatch")
	proto.RegisterType((*PortMatch)(nil), "istio.rbac.v1alpha1.PortMatch")
	proto.RegisterType((*Permission)(nil), "istio.rbac.v1alpha1.Permission")
	proto.RegisterType((*Permission_Condition)(nil), "istio.rbac.v1alpha1.Permission.Condition")
	proto.RegisterType((*Principal)(nil), "istio.rbac.v1alpha1.Principal")
	proto.RegisterType((*Principal_Authenticated)(nil), "istio.rbac.v1alpha1.Principal.Authenticated")
	proto.RegisterType((*Principal_Attribute)(nil), "istio.rbac.v1alpha1.Principal.Attribute")
	proto.RegisterEnum("istio.rbac.v1alpha1.RBAC_Action", RBAC_Action_name, RBAC_Action_value)
}

func init() { proto.RegisterFile("rbac/v1alpha1/rbac.proto", fileDescriptor0) }

var fileDescriptor0 = []byte{
	// 733 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x94, 0x55, 0x5d, 0x6b, 0xdb, 0x48,
	0x14, 0xb5, 0x22, 0xdb, 0x89, 0xae, 0x91, 0xe3, 0x9d, 0x5d, 0x16, 0xe1, 0x4d, 0x76, 0xbd, 0x5a,
	0x96, 0xba, 0x50, 0x6c, 0xe2, 0x96, 0x12, 0x4a, 0x3f, 0xb0, 0xdd, 0x50, 0x07, 0x92, 0x36, 0x9d,
	0x3e, 0xf4, 0xeb, 0xc1, 0x4c, 0xa4, 0x49, 0x3c, 0xd4, 0x96, 0x86, 0x99, 0x71, 0x48, 0xde, 0x0b,
	0x7d, 0xe8, 0x8f, 0xe8, 0xbf, 0xe8, 0x1f, 0xeb, 0x7b, 0x29, 0x9a, 0x91, 0x14, 0x99, 0x2a, 0x0d,
	0x79, 0xd3, 0x9d, 0x7b, 0xcf, 0xb9, 0x73, 0xcf, 0xb9, 0x92, 0xc0, 0x13, 0xc7, 0x24, 0xe8, 0x9f,
	0xed, 0x90, 0x39, 0x9f, 0x91, 0x9d, 0x7e, 0x12, 0xf5, 0xb8, 0x88, 0x55, 0x8c, 0x7e, 0x67, 0x52,
	0xb1, 0xb8, 0xa7, 0x4f, 0xb2, 0xbc, 0xff, 0xdd, 0x82, 0x2a, 0x1e, 0x0d, 0xc7, 0x68, 0x17, 0xea,
	0x24, 0x50, 0x2c, 0x8e, 0x3c, 0xab, 0x63, 0x75, 0x9b, 0x83, 0x4e, 0xaf, 0xa4, 0xbc, 0x97, 0x94,
	0xf6, 0x86, 0xba, 0x0e, 0xa7, 0xf5, 0x68, 0x0c, 0x1b, 0x3c, 0x9e, 0xb3, 0x80, 0x51, 0xe9, 0xad,
	0x75, 0xec, 0x6e, 0x63, 0x70, 0xeb, 0x6a, 0xec, 0x51, 0x5a, 0xb9, 0x17, 0x29, 0x71, 0x81, 0x73,
	0x60, 0xfb, 0x0d, 0xb8, 0x2b, 0x29, 0xd4, 0x02, 0xfb, 0x03, 0xbd, 0xd0, 0x97, 0x71, 0x70, 0xf2,
	0x88, 0x76, 0xa0, 0x76, 0x46, 0xe6, 0x4b, 0xea, 0xad, 0x75, 0xac, 0x6e, 0x63, 0xf0, 0x57, 0x69,
	0x13, 0x4d, 0x72, 0x81, 0x4d, 0xe5, 0x83, 0xb5, 0x5d, 0xcb, 0xdf, 0x86, 0xba, 0xb9, 0x30, 0x72,
	0xa0, 0x36, 0x3c, 0x38, 0x78, 0xf1, 0xba, 0x55, 0x41, 0x1b, 0x50, 0x7d, 0xba, 0xf7, 0xfc, 0x6d,
	0xcb, 0xf2, 0x3f, 0x5a, 0xd0, 0x78, 0xa5, 0x04, 0x8b, 0x4e, 0x0f, 0x89, 0x0a, 0x66, 0xc8, 0x83,
	0xba, 0x64, 0x0b, 0x3e, 0xa7, 0xa6, 0xf5, 0xa4, 0x82, 0xd3, 0x38, 0xc9, 0x70, 0x41, 0x4f, 0xd8,
	0xb9, 0xbe, 0x80, 0xce, 0x98, 0x58, 0x63, 0x96, 0x27, 0x49, 0xc6, 0xce, 0x31, 0x3a, 0x46, 0x7f,
	0x42, 0x4d, 0xd0, 0x53, 0x7a, 0xee, 0x55, 0xd3, 0x84, 0x09, 0x47, 0x9b, 0xe0, 0x2e, 0x92, 0x76,
	0x53, 0x4e, 0x94, 0xa2, 0x22, 0xf2, 0x3f, 0x5b, 0x50, 0x37, 0x77, 0x47, 0x43, 0x68, 0x70, 0x2a,
	0x16, 0x4c, 0x4a, 0x16, 0x47, 0xd2, 0xb3, 0xb4, 0xa4, 0xff, 0x94, 0x4f, 0x9b, 0xd7, 0xe1, 0x22,
	0x06, 0x3d, 0x06, 0xe0, 0x82, 0x45, 0x01, 0xe3, 0x64, 0x9e, 0x99, 0xf2, 0x77, 0x39, 0x43, 0x56,
	0x86, 0x0b, 0x08, 0xff, 0x3d, 0xb8, 0x87, 0x84, 0x6b, 0x23, 0x8c, 0x2a, 0x3f, 0xbb, 0xb1, 0x0b,
	0x75, 0xad, 0x71, 0x46, 0x5f, 0xbe, 0x2f, 0x05, 0x65, 0x71, 0x5a, 0xef, 0xbf, 0x04, 0x67, 0xcc,
	0x42, 0x81, 0x49, 0x74, 0x4a, 0xd1, 0xff, 0xd0, 0x24, 0x61, 0x28, 0xa8, 0x94, 0xd3, 0x54, 0x5c,
	0xd3, 0xc3, 0x4d, 0x4f, 0x8f, 0x8c, 0xc2, 0xdb, 0xc9, 0x40, 0xc9, 0xd3, 0x74, 0x4e, 0x23, 0xad,
	0xbf, 0x8b, 0x1d, 0x73, 0x72, 0x40, 0x23, 0xff, 0x09, 0xac, 0xef, 0x73, 0x73, 0xd3, 0x7b, 0x50,
	0x0b, 0x58, 0x28, 0x32, 0xdd, 0xca, 0xa7, 0xce, 0xfb, 0x63, 0x53, 0xec, 0xff, 0x0b, 0xce, 0x51,
	0x2c, 0x94, 0xa1, 0xf8, 0x03, 0x6a, 0x3c, 0x16, 0xca, 0x50, 0xb8, 0xd8, 0x04, 0xfe, 0x17, 0x1b,
	0xe0, 0x52, 0x6f, 0x74, 0x1f, 0x6a, 0x9c, 0xa8, 0x59, 0xd6, 0xe7, 0xfa, 0xf1, 0x4d, 0x39, 0xf2,
	0x60, 0x7d, 0x41, 0xd5, 0x2c, 0x0e, 0x8d, 0x70, 0x0e, 0xce, 0x42, 0xb4, 0x0f, 0x10, 0xc4, 0x51,
	0xc8, 0x94, 0xb6, 0xdd, 0xd6, 0xb4, 0xb7, 0xaf, 0xb1, 0xbd, 0x37, 0xce, 0x10, 0xb8, 0x00, 0x6e,
	0x7f, 0xb3, 0xc0, 0xc9, 0x33, 0xe8, 0x21, 0xd4, 0x67, 0x94, 0x84, 0x54, 0x68, 0x6d, 0x1b, 0x03,
	0xbf, 0x94, 0x74, 0xc5, 0xf0, 0x64, 0x85, 0x0d, 0x06, 0x3d, 0x83, 0xcd, 0x90, 0x4a, 0xc5, 0x22,
	0x92, 0x90, 0x4d, 0x19, 0x97, 0xe9, 0x0b, 0xb8, 0x55, 0x4a, 0x93, 0xfa, 0x30, 0xa9, 0xe0, 0x66,
	0x01, 0xb6, 0xcf, 0x25, 0x3a, 0x84, 0xdf, 0x8a, 0x44, 0x46, 0x62, 0x5b, 0x53, 0x5d, 0xb1, 0x9b,
	0x99, 0x23, 0x93, 0x0a, 0x6e, 0x15, 0xa0, 0xc9, 0xb9, 0x1c, 0xb5, 0xa0, 0x99, 0x4f, 0x3c, 0x95,
	0x9c, 0x06, 0xfe, 0x27, 0x1b, 0x9c, 0x7c, 0x9f, 0x11, 0x06, 0x97, 0x2c, 0xd5, 0x8c, 0x46, 0x8a,
	0x05, 0x44, 0xd1, 0x30, 0x1d, 0xfe, 0xce, 0xaf, 0x5f, 0x83, 0xde, 0xb0, 0x88, 0xc1, 0xab, 0x14,
	0x68, 0x02, 0x40, 0x94, 0x12, 0xec, 0x78, 0xa9, 0xf2, 0xc5, 0xef, 0x5e, 0x47, 0x98, 0x01, 0x70,
	0x01, 0xdb, 0xfe, 0x0f, 0xdc, 0x95, 0x4e, 0x08, 0x41, 0x35, 0x22, 0x8b, 0xf4, 0xab, 0x83, 0xf5,
	0x73, 0xfb, 0xab, 0x05, 0x4e, 0x0e, 0x47, 0x6d, 0x58, 0x97, 0x54, 0x9c, 0xb1, 0xe0, 0xf2, 0xd3,
	0x94, 0x1d, 0xa0, 0x47, 0x00, 0x32, 0x5e, 0x8a, 0x80, 0xde, 0xc0, 0x1f, 0xc7, 0x20, 0x12, 0x6b,
	0x2e, 0x37, 0xc4, 0xbe, 0xf9, 0x86, 0x24, 0x4e, 0xe4, 0x93, 0x69, 0x27, 0x46, 0x5b, 0xef, 0xda,
	0x86, 0x80, 0xc5, 0x7d, 0xc2, 0x59, 0x7f, 0xe5, 0x9f, 0x74, 0x5c, 0xd7, 0xff, 0xa3, 0xbb, 0x3f,
	0x02, 0x00, 0x00, 0xff, 0xff, 0x0d, 0x1e, 0x0f, 0x15, 0xab, 0x06, 0x00, 0x00,
}
