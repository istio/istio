# Istio API Guidelines

This page defines the design guidelines for Istio APIs. They apply to 
[Kubernetes Custom Resource Definitions](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/)
and all [proto files](https://developers.google.com/protocol-buffers/) that are used to
configure Istio components through the [Mesh Configuration Protocol(MCP)](https://docs.google.com/document/d/1o2-V4TLJ8fJACXdlsnxKxDv2Luryo48bAhR8ShxE5-k/edit).

Since MCP is based on _proto3_ and _gRPC_, we will use
Google's [API Design Guide](https://cloud.google.com/apis/design) as
the baseline for protos. Because Envoy APIs also uses the same baseline, the
commonality across Envoy, Istio, proto3 and gRPC will greatly help
developer experience in the long term.
 
In addition to Google's guide, the following conventions should be
followed for Istio APIs.

## Contents

- [Proto Guidelines](#proto-guidelines)
  - [Style](#style)
  - [Basic Proto Versioning](#basic-proto-ersioning)
- [CRD Guidelines](#crd-guidelines)
  - [Style](#style-1)
  - [Basic CRD Versioning](#basic-crd-versioning)

## Proto Guidelines
This section captures guidelines that apply to the proto form of the configuration resources.

### Style

#### Placement
* **Do** place new API protos under ```istio.io/api/<area>/<version>``` folder.
* **Prefer** complete words for file names.
  * ```config.proto // Not cfg.proto!```

#### Package Names

* **Do** use `lowercase` without any `_`.
* **Do** use singular words
* **Do** use the name pattern ```istio.<area>.<version>```.

```proto
package istio.networking.v1alpha3;
```
  
  
#### Message/Enum/Method Names
* **Do not** use embedded acronyms. See [#364](https://github.com/istio/api/issues/364) for details.

```proto
message HttpRequest {/*...*/}  // Not HTTPRequest!

rpc DoHttpRequest(/*...*/)     // Not DoHTTPRequest!

enum HttpStatusCodes {/*...*/} // Not HTTPStatusCodes!
```


#### Messages
* **Do** use ```CamelCase``` for message names.
  * ```proto 
    message MyMessage {...}
    ```

#### Fields
* **Do** use ```lowercase_with_underscore``` for field names: 
  * ```proto 
    string display_name = 1;
    ```
* **Do** use plural names for repeated fields:
  * ```proto
    repeated rule rules = 2;
    ```
* **Do not** use postpositive adjectives in names.
  * ```proto
    repeated Items collected_items = 3; // Not items_collected!
    ```
 
#### Enums
* **Do** use ```CamelCase``` for  types names.
  * ```proto
    enum Types {/*...*/}
    ```
* **Do** use `UPPERCASE_WITH_UNDERSCORE` for enum names: 
  * ```proto
    enum Types {INT_TYPE = 1;}
    ```
* **Do** have an enum entry for value ```0```.
  * When a new field with an enum type is introduced to a proto, when reading 
  the older versions of the data, it will be defaulted to ```0``` value.
* **Do** name the ```0``` value either as ```<ENUMTYPE>_UNDEFINED``` or use a sane,
well-known value that will be considered as default.
  * ```proto
    enum Types { TYPE_UNDEFINED = 0;}
    ```

### Basic Proto Versioning

Protocol Buffers have well-defined semantics for coping with version changes. In essense, the unknown fields
get ignored, and additive, and non-breaking changes are acceptable.

In addition to the standard proto versioning semantics, Istio tooling imposes its own restrictions, as CRD
to proto conversion system in Istio depends on names in certain situations.

The following rules captures the basic rules that should be followed when making changes to Istio config 
protos.

* **Do not** change field numbers.
  * Proto depends on field numbers to find fields.
  * ```proto
        // Field number has changed from 1 to 2!
        
        // string field = 1; // Deleted
        string field = 2;         
    ```
* **Do not** rename fields.
  * Our tooling automatically maps YAML fields to proto fields.
  * ```proto
        // Field name has changed!
        
        // string old_field = 1;
        string new_field = 1; 
    ```
* **Do not** change cardinality of fields.
  * ```proto
        // Field cardinality has changed!
        
        // string should_have_been_plural = 1;
        repeated string should_have_been_plural = 1;
    ```
* **Do not** rename top-level protos that map to CRD config types.
  * Istio tooling depends on the name of the top-level protos to map CRDs to the matching proto
  counterparts.
  ```proto
     // Top-level proto name has changed!
   
     // message Rule {
     message PolicyRule {
          // ...
     }
  ``` 
* **Avoid** changing/renaming field types.
  * If the field types changes, the new type **must** be structurally equivalent to the old.
  ```proto
      // Field type changed from Boo to Zoo!
      // Boo and Zoo must be structurally equivalent!
      message Foo {
         // Boo boo = 1;  
         Zoo boo = 2;
      }
    ```
* **Do not** rename enum names, or change values.
  * Istio tooling depends on names to convert enums from CRD form to proto.
  * ```proto   
        enum Types {
           // Enum name has changed!
           // Foo = 1;
           Bar = 1;

           // Enum value has changed!
           // Baz = 2;
           Baz = 3;

        }    
    ```

## CRD Guidelines

### Style
* **Do** use the name pattern ```<area>.istio.io``` for API group names.
* **Do** use the version from the proto package as the API version.
* **Do** use the top-level proto Message name as the ```kind``` of the CRD.

```yaml
# A custom resource that describes a Gateway resource
apiVersion: networking.istio.io/v1alpha3
kind: Gateway 
# ...
```

Matches to:
```proto
package istio.networking.v1alpha3;
message Gateway {
    // ...
}
```

### Basic CRD Versioning

Istio APIs should use a simple versioning strategy based on
major versions and releases, such as `v1alpha`, `v2beta`, or
`v3`. Within each release, there should not be any breaking
change to released features, such as changing the type of
a field type, renaming a field, or changing a field number.
Breaking changes are allowed between different releases,
such as `v1alpha1` and `v1alpha2`.

Deprecating a feature in an API release is allowed by following
the applicable deprecation process. The reason to allow
deprecation of individual features in a release is that it is
significantly cheaper and simpler for everyone involved. In
practice, it works out much better than deprecating an entire
API version.
