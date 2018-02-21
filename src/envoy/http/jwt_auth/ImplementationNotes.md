# Implementation Notes

## JWT library

- `jwt.h`, `jwt.cc`

- ### Status
  - enum to represent failure reason.
  - #### TODO:
    - categorize failure reason 
      (e.g. JWT parse error, public key parse error, or unmatch between JWT and key)

- ### Interfaces
  - `Jwt`
  - `Pubkeys`
    - It holding several public keys (for e.g. JWKs = array of JWK)
  - `Verifier` 

- ### Public key formats:
  - PEM, JWKs

- ### signing algorithm:
  - RS256

- ### TODO:
  - support other public key format / signing algorithm

## HTTP filter

This consists of some parts:

### Loading Envoy config

  - Filter object has a `JwtAuthConfig` object keeping the config.
  - `JwtAuthConfig()` in `config.cc` is loading the filter config in Envoy config file. 
  - It calls `IssuerInfo()` (in `config.cc`) for each issuer, which loads the config for each issuer.
    - In the case URI is provided, 
      `IssuerInfo` keeps the URI and cluster name and the public key will be fetched later, 
      namely in `JwtVerificationFilter::decodeHeaders()`.
  
### Fetching public key
  - In `JwtVerificationFilter::decodeHeaders()`, 
    requests for public keys to issuers are made if needed, using `AsyncClientCallbacks` in `config.cc`.
  - `JwtVerificationFilter::ReceivePubkey()` is passed to AsyncClient as a callback, 
    which will call `JwtVerificationFilter::CompleteVerification()` after all responses are received.
   
  - #### Issue: 
    - https://github.com/istio/proxy/issues/468
  
  - #### TODO:
    - add tests for config loading
  
### Verifying JWT & Making response
  - `JwtVerificationFilter::CompleteVerification()` calls
    `JwtVerificationFilter::Verify()`, which verifies the JWT in HTTP header
    and returns `OK` or failure reason.
    
  - `JwtVerificationFilter::CompleteVerification()` passes or declines request.

### Other TODO:
  - add Error Log
  - add integration tests
