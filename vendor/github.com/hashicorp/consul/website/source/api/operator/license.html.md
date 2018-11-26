---
layout: api
page_title: License - Operator - HTTP API
sidebar_current: api-operator-license
description: |-
  The /operator/license endpoints allow for setting and retrieving the Consul
  Enterprise License.
---

# License - Operator HTTP API

~> **Enterprise Only!** This API endpoint and functionality only exists in
Consul Enterprise. This is not present in the open source version of Consul.

The licensing functionality described here is available only in
[Consul Enterprise](https://www.hashicorp.com/products/consul/) version 1.1.0 and later.

## Getting the Consul License

This endpoint gets information about the current license.

| Method | Path                         | Produces                   |
| ------ | ---------------------------- | -------------------------- |
| `GET` | `/operator/license`           | `application/json`         |

The table below shows this endpoint's support for
[blocking queries](/api/index.html#blocking-queries),
[consistency modes](/api/index.html#consistency-modes),
[agent caching](/api/index.html#agent-caching), and
[required ACLs](/api/index.html#acls).

| Blocking Queries | Consistency Modes | Agent Caching | ACL Required     |
| ---------------- | ----------------- | ------------- | ---------------- |
| `NO`             | `all`             | `none`        | `none`           |

### Parameters

- `dc` `(string: "")` - Specifies the datacenter whose license should be retrieved.
  This will default to the datacenter of the agent serving the HTTP request.
  This is specified as a URL query parameter.

### Sample Request

```text
$ curl \
    http://127.0.0.1:8500/v1/operator/license
```

### Sample Response

```json
{
    "Valid": true,
    "License": {
        "license_id": "2afbf681-0d1a-0649-cb6c-333ec9f0989c",
        "customer_id": "0259271d-8ffc-e85e-0830-c0822c1f5f2b",
        "installation_id": "*",
        "issue_time": "2018-05-21T20:03:35.911567355Z",
        "start_time": "2018-05-21T04:00:00Z",
        "expiration_time": "2019-05-22T03:59:59.999Z",
        "product": "consul",
        "flags": {
            "package": "premium"
        },
        "features": [
            "Automated Backups",
            "Automated Upgrades",
            "Enhanced Read Scalability",
            "Network Segments",
            "Redundancy Zone",
            "Advanced Network Federation"
        ],
        "temporary": false
    },
    "Warnings": []
}
```

## Updating the Consul License

This endpoint updates the Consul license and returns some of the
license contents as well as any warning messages regarding its validity.

| Method | Path                         | Produces                   |
| ------ | ---------------------------- | -------------------------- |
| `PUT` | `/operator/license`           | `application/json`         |

The table below shows this endpoint's support for
[blocking queries](/api/index.html#blocking-queries),
[consistency modes](/api/index.html#consistency-modes),
[agent caching](/api/index.html#agent-caching), and
[required ACLs](/api/index.html#acls).

| Blocking Queries | Consistency Modes | Agent Caching | ACL Required     |
| ---------------- | ----------------- | ------------- | ---------------- |
| `NO`             | `none`            | `none`        | `operator:write` |

### Parameters

- `dc` `(string: "")` - Specifies the datacenter whose license should be updated.
  This will default to the datacenter of the agent serving the HTTP request.
  This is specified as a URL query parameter.

### Sample Payload

The payload is the raw license blob.

### Sample Request

```text
$ curl \
    --request PUT \
    --data @consul.license \
    http://127.0.0.1:8500/v1/operator/license
```

### Sample Response

```json
{
    "Valid": true,
    "License": {
        "license_id": "2afbf681-0d1a-0649-cb6c-333ec9f0989c",
        "customer_id": "0259271d-8ffc-e85e-0830-c0822c1f5f2b",
        "installation_id": "*",
        "issue_time": "2018-05-21T20:03:35.911567355Z",
        "start_time": "2018-05-21T04:00:00Z",
        "expiration_time": "2019-05-22T03:59:59.999Z",
        "product": "consul",
        "flags": {
            "package": "premium"
        },
        "features": [
            "Automated Backups",
            "Automated Upgrades",
            "Enhanced Read Scalability",
            "Network Segments",
            "Redundancy Zone",
            "Advanced Network Federation"
        ],
        "temporary": false
    },
    "Warnings": []
}
```
