# mTLS Server Sample

This sample implements mutual TLS (mTLS) authentication, requiring both client and server certificates for secure communication. This is useful for testing Istio's mTLS capabilities.

## Overview

The mTLS server:

- Requires client certificate verification (mutual TLS)
- Validates client certificates against a configured Certificate Authority (CA)
- Provides detailed information about the client certificate in HTTP responses
- Supports configurable certificate paths and listen address via environment variables
- Supports both text and JSON response formats with content negotiation
- Provides multiple endpoints: `/` for full information and `/subject` for certificate subject only
- Can be configured with server name and namespace for multi-service deployments

## Configuration

The server can be configured using the following environment variables:

| Environment Variable | Default Value | Description |
|---------------------|---------------|-------------|
| `SERVER_CERT` | `server.crt` | Path to the server certificate file |
| `SERVER_KEY` | `server.key` | Path to the server private key file |
| `CA_CERT` | `ca.crt` | Path to the Certificate Authority certificate file |
| `LISTEN_ADDR` | `:8443` | Address and port to listen on |
| `SERVER_NAME` | (empty) | Name of the server (included in responses) |
| `SERVER_NAMESPACE` | (empty) | Namespace of the server (included in responses) |
| `ALWAYS_JSON` | `false` | If set to `true`, always return JSON responses regardless of Accept header |

## Endpoints

The server provides the following endpoints:

- `/` - Returns detailed client certificate information and request headers
- `/subject` - Returns only the client certificate subject (also sets `mtls-client-subject` header)

Both endpoints support content negotiation:

- Text response - (default): Human-readable format
- JSON response - Structured data format (triggered by `Accept: application/json` header or env variable `ALWAYS_JSON="true"`)

## Building and Running

```bash
cd samples/mtls-server/src/
docker build -t mtls-server . --tag localhost:5000/examples-mtls-server:latest
```

## Expected Responses

**Main endpoint text response:**

```text
Hello from mTLS server!

Client certificate subject: CN=client,O=Example Org
Client certificate issuer: CN=Example CA,O=Example Org
Client certificate serial number: 123456789
Client certificate DNS names: [client.example.com]
Client certificate NotBefore: 2024-01-01 00:00:00 +0000 UTC
Client certificate NotAfter: 2025-01-01 00:00:00 +0000 UTC
```

**Main endpoint JSON response:**

```json
{
  "Name": "",
  "Namespace": "",
  "Client": {
    "Subject": "CN=client,O=Example Org",
    "Issuer": "CN=Example CA,O=Example Org",
    "Serial": "123456789",
    "DNSNames": ["client.example.com"],
    "NotBefore": "2024-01-01T00:00:00Z",
    "NotAfter": "2025-01-01T00:00:00Z"
  },
  "Headers": {
    "Accept": ["application/json"],
    "User-Agent": ["curl/7.68.0"]
  }
}
```

**Subject endpoint text response:**

```text
CN=client,O=Example Org
```

**Subject endpoint JSON response:**

```json
{
  "subject": "CN=client,O=Example Org"
}
```
