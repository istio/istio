# Protocol Documentation
<a name="top"></a>

## Table of Contents

- [node.proto](#node.proto)
    - [AttestRequest](#spire.api.node.AttestRequest)
    - [AttestResponse](#spire.api.node.AttestResponse)
    - [Bundle](#spire.api.node.Bundle)
    - [FetchJWTSVIDRequest](#spire.api.node.FetchJWTSVIDRequest)
    - [FetchJWTSVIDResponse](#spire.api.node.FetchJWTSVIDResponse)
    - [FetchX509CASVIDRequest](#spire.api.node.FetchX509CASVIDRequest)
    - [FetchX509CASVIDResponse](#spire.api.node.FetchX509CASVIDResponse)
    - [FetchX509SVIDRequest](#spire.api.node.FetchX509SVIDRequest)
    - [FetchX509SVIDRequest.CsrsEntry](#spire.api.node.FetchX509SVIDRequest.CsrsEntry)
    - [FetchX509SVIDResponse](#spire.api.node.FetchX509SVIDResponse)
    - [JSR](#spire.api.node.JSR)
    - [JWTSVID](#spire.api.node.JWTSVID)
    - [X509SVID](#spire.api.node.X509SVID)
    - [X509SVIDUpdate](#spire.api.node.X509SVIDUpdate)
    - [X509SVIDUpdate.BundlesEntry](#spire.api.node.X509SVIDUpdate.BundlesEntry)
    - [X509SVIDUpdate.SvidsEntry](#spire.api.node.X509SVIDUpdate.SvidsEntry)
  
  
  
    - [Node](#spire.api.node.Node)
  

- [Scalar Value Types](#scalar-value-types)



<a name="node.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## node.proto



<a name="spire.api.node.AttestRequest"></a>

### AttestRequest
Represents a request to attest the node.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| attestation_data | [spire.common.AttestationData](#spire.common.AttestationData) |  | A type which contains attestation data for specific platform. |
| csr | [bytes](#bytes) |  | Certificate signing request. |
| response | [bytes](#bytes) |  | Attestation challenge response |






<a name="spire.api.node.AttestResponse"></a>

### AttestResponse
Represents a response that contains  map of signed SVIDs and an array of
all current Registration Entries which are relevant to the caller SPIFFE ID


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| svid_update | [X509SVIDUpdate](#spire.api.node.X509SVIDUpdate) |  | It includes a map of signed SVIDs and an array of all current Registration Entries which are relevant to the caller SPIFFE ID. |
| challenge | [bytes](#bytes) |  | This is a challenge issued by the server to the node. If populated, the node is expected to respond with another AttestRequest with the response. This field is mutually exclusive with the update field. |






<a name="spire.api.node.Bundle"></a>

### Bundle
Trust domain bundle


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | bundle identifier, i.e. the SPIFFE ID for the trust domain |
| ca_certs | [bytes](#bytes) |  | bundle data (ASN.1 encoded X.509 certificates) |






<a name="spire.api.node.FetchJWTSVIDRequest"></a>

### FetchJWTSVIDRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| jsr | [JSR](#spire.api.node.JSR) |  | The JWT signing request |






<a name="spire.api.node.FetchJWTSVIDResponse"></a>

### FetchJWTSVIDResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| svid | [JWTSVID](#spire.api.node.JWTSVID) |  | The signed JWT-SVID |






<a name="spire.api.node.FetchX509CASVIDRequest"></a>

### FetchX509CASVIDRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| csr | [bytes](#bytes) |  |  |






<a name="spire.api.node.FetchX509CASVIDResponse"></a>

### FetchX509CASVIDResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| svid | [X509SVID](#spire.api.node.X509SVID) |  |  |
| bundle | [spire.common.Bundle](#spire.common.Bundle) |  |  |






<a name="spire.api.node.FetchX509SVIDRequest"></a>

### FetchX509SVIDRequest
Represents a request with a list of CSR.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| DEPRECATED_csrs | [bytes](#bytes) | repeated | A list of CSRs (deprecated, use `csrs` map instead) |
| csrs | [FetchX509SVIDRequest.CsrsEntry](#spire.api.node.FetchX509SVIDRequest.CsrsEntry) | repeated | A map of CSRs keyed by entry ID |






<a name="spire.api.node.FetchX509SVIDRequest.CsrsEntry"></a>

### FetchX509SVIDRequest.CsrsEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [bytes](#bytes) |  |  |






<a name="spire.api.node.FetchX509SVIDResponse"></a>

### FetchX509SVIDResponse
Represents a response that contains  map of signed SVIDs and an array
of all current Registration Entries which are relevant to the caller SPIFFE ID.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| svid_update | [X509SVIDUpdate](#spire.api.node.X509SVIDUpdate) |  | It includes a map of signed SVIDs and an array of all current Registration Entries which are relevant to the caller SPIFFE ID. |






<a name="spire.api.node.JSR"></a>

### JSR
JSR is a JWT SVID signing request.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| spiffe_id | [string](#string) |  | SPIFFE ID of the workload |
| audience | [string](#string) | repeated | List of intended audience |
| ttl | [int32](#int32) |  | Time-to-live in seconds. If unspecified the JWT SVID will be assigned a default time-to-live by the server. |






<a name="spire.api.node.JWTSVID"></a>

### JWTSVID
JWTSVID is a signed JWT-SVID with fields lifted out for convenience.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| token | [string](#string) |  | JWT-SVID JWT token |
| expires_at | [int64](#int64) |  | SVID expiration timestamp (seconds since Unix epoch) |
| issued_at | [int64](#int64) |  | SVID issuance timestamp (seconds since Unix epoch) |






<a name="spire.api.node.X509SVID"></a>

### X509SVID
A type which contains the &#34;Spiffe Verifiable Identity Document&#34; and
a TTL indicating when the SVID expires.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| cert_chain | [bytes](#bytes) |  | X509 SVID and intermediates necessary to form a chain of trust back to a root CA in the bundle. |
| expires_at | [int64](#int64) |  | SVID expiration timestamp (in seconds since Unix epoch) |






<a name="spire.api.node.X509SVIDUpdate"></a>

### X509SVIDUpdate
A message returned by the Spire Server, which includes a map of signed SVIDs and
a list of all current Registration Entries which are relevant to the caller SPIFFE ID.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| svids | [X509SVIDUpdate.SvidsEntry](#spire.api.node.X509SVIDUpdate.SvidsEntry) | repeated | A map containing SVID values keyed by: - SPIFFE ID in message &#39;AttestResponse&#39; (Map[SPIFFE_ID] =&gt; SVID) - Entry ID in message &#39;FetchX509SVIDResponse&#39; (Map[Entry_ID] =&gt; SVID) |
| registration_entries | [spire.common.RegistrationEntry](#spire.common.RegistrationEntry) | repeated | A type representing a curated record that the Spire Server uses to set up and manage the various registered nodes and workloads that are controlled by it. |
| bundles | [X509SVIDUpdate.BundlesEntry](#spire.api.node.X509SVIDUpdate.BundlesEntry) | repeated | Trust bundles associated with the SVIDs, keyed by trust domain SPIFFE ID. Bundles included are the trust bundle for the server trust domain and any federated trust domain bundles applicable to the SVIDs. Supersedes the deprecated `bundle` field. |






<a name="spire.api.node.X509SVIDUpdate.BundlesEntry"></a>

### X509SVIDUpdate.BundlesEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [spire.common.Bundle](#spire.common.Bundle) |  |  |






<a name="spire.api.node.X509SVIDUpdate.SvidsEntry"></a>

### X509SVIDUpdate.SvidsEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [X509SVID](#spire.api.node.X509SVID) |  |  |





 

 

 


<a name="spire.api.node.Node"></a>

### Node


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| Attest | [AttestRequest](#spire.api.node.AttestRequest) stream | [AttestResponse](#spire.api.node.AttestResponse) stream | Attest the node, get base node SVID. |
| FetchX509SVID | [FetchX509SVIDRequest](#spire.api.node.FetchX509SVIDRequest) stream | [FetchX509SVIDResponse](#spire.api.node.FetchX509SVIDResponse) stream | Get Workload, Node Agent certs and CA trust bundles. Also used for rotation Base Node SVID or the Registered Node SVID used for this call) List can be empty to allow Node Agent cache refresh). |
| FetchJWTSVID | [FetchJWTSVIDRequest](#spire.api.node.FetchJWTSVIDRequest) | [FetchJWTSVIDResponse](#spire.api.node.FetchJWTSVIDResponse) | Fetches a signed JWT-SVID for a workload intended for a specific audience. |
| FetchX509CASVID | [FetchX509CASVIDRequest](#spire.api.node.FetchX509CASVIDRequest) | [FetchX509CASVIDResponse](#spire.api.node.FetchX509CASVIDResponse) | Fetches an X509 CA SVID for a downstream SPIRE server. |

 



## Scalar Value Types

| .proto Type | Notes | C++ Type | Java Type | Python Type |
| ----------- | ----- | -------- | --------- | ----------- |
| <a name="double" /> double |  | double | double | float |
| <a name="float" /> float |  | float | float | float |
| <a name="int32" /> int32 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint32 instead. | int32 | int | int |
| <a name="int64" /> int64 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint64 instead. | int64 | long | int/long |
| <a name="uint32" /> uint32 | Uses variable-length encoding. | uint32 | int | int/long |
| <a name="uint64" /> uint64 | Uses variable-length encoding. | uint64 | long | int/long |
| <a name="sint32" /> sint32 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int32s. | int32 | int | int |
| <a name="sint64" /> sint64 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int64s. | int64 | long | int/long |
| <a name="fixed32" /> fixed32 | Always four bytes. More efficient than uint32 if values are often greater than 2^28. | uint32 | int | int |
| <a name="fixed64" /> fixed64 | Always eight bytes. More efficient than uint64 if values are often greater than 2^56. | uint64 | long | int/long |
| <a name="sfixed32" /> sfixed32 | Always four bytes. | int32 | int | int |
| <a name="sfixed64" /> sfixed64 | Always eight bytes. | int64 | long | int/long |
| <a name="bool" /> bool |  | bool | boolean | boolean |
| <a name="string" /> string | A string must always contain UTF-8 encoded or 7-bit ASCII text. | string | String | str/unicode |
| <a name="bytes" /> bytes | May contain any arbitrary sequence of bytes. | string | ByteString | str |

