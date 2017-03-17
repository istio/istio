# Attributes & Policy Evaluation

Istio uses *attributes* to describe runtime behavior of services running in the mesh. Attributes are named and typed pieces of metadata
describing ingress and egress traffic and the environment this traffic occurs in. An Istio attribute carries a specific piece
of information such as the error code of an API request, the latency of an API request, or the
original IP address of a TCP connection.
 
Istio's policy evaluation model operates on attributes. For example, access control is configured by
specifying policies against particular attribute values.

A given Istio deployment has a fixed vocabulary of attributes that it understands. The specific vocabulary is
determined by the set of attribute producers being used in the deployment. The primary attribute producer in Istio
is the proxy, although the mixer and services can also introduce attributes.

Attribute producers declare the set of attributes they produce using the [`AttributeDescriptor`](https://raw.githubusercontent.com/istio/api/master/mixer/v1/config/descriptor/attribute_descriptor.proto)
message:

    message AttributeDescriptor {
      // The name of this descriptor, referenced from individual attribute instances and other
      // descriptors.
      string name = 1;
    
      // An optional human-readable description of the attribute's purpose.
      string description = 2;
    
      // The type of data carried by attributes
      ValueType value_type = 3;
    }

    enum ValueType {
        // Invalid, default value.
        VALUE_TYPE_UNSPECIFIED = 0;
    
        // An undiscriminated variable-length string.
        STRING = 1;
    
        // An undiscriminated 64-bit signed integer.
        INT64 = 2;
    
        // An undiscriminated 64-bit floating-point value.
        DOUBLE = 3;
    
        // An undiscriminated boolean value.
        BOOL = 4;
    
        // A point in time.
        TIMESTAMP = 5;
    
        // An IP address.
        IP_ADDRESS = 6;
    
        // An email address.
        EMAIL_ADDRESS = 7;
    
        // A URI.
        URI = 8;
    
        // A DNS name.
        DNS_NAME = 9;
    
        // A span between two points in time.
        DURATION = 10;
    
        // A map string -> string, typically used by headers.
        STRING_MAP = 11;
    }

When an attribute is used in Istio, its name is given along with a value. The value must be of the type declared by the corresponding descriptor. This
type checking makes it possible for the Istio system to statically detect or prevent many configuration errors.
