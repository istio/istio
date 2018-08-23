---
layout: "api"
page_title: "/sys/license - HTTP API"
sidebar_current: "docs-http-system-license"
description: |-
  The `/sys/license` endpoint is used to view and update the license used in 
  Vault.
---

# `/sys/license`

~> **Enterprise Only** – These endpoints require Vault Enterprise.

The `/sys/license` endpoint is used to view and update the license used in 
Vault.

## Read License

This endpoint returns information about the currently installed license.

| Method   | Path                         | Produces               |
| :------- | :--------------------------- | :--------------------- |
| `GET`    | `/sys/license`                | `200 application/json` |

### Sample Request

```
$ curl \
    --header "X-Vault-Token: ..." \
    http://127.0.0.1:8200/v1/sys/license
```

### Sample Response

```json
{
  "data": {
    "expiration_time": "2017-11-14T16:34:36.546753-05:00",
    "features": [
      "UI",
      "HSM",
      "Performance Replication",
      "DR Replication"
    ],
    "license_id": "temporary",
    "start_time": "2017-11-14T16:04:36.546753-05:00"
  },
  "warnings": [
    "time left on license is 29m33s"
  ]
}
```

## Install License

This endpoint is used to install a license into Vault.

| Method   | Path                         | Produces               |
| :------- | :--------------------------- | :--------------------- |
| `PUT`    | `/sys/license`                | `204 (empty body)` |

### Parameters

- `text` `(string: <required>)` – The text of the license.

*DR Secondary Specific Parameters*

  - `dr_operation_token` `(string: <required>)` - DR operation token used to authorize this request.


### Sample Payload

```json
{
  "text": "01ABCDEFG..."
}
```

### Sample Request

```
$ curl \
    --header "X-Vault-Token: ..." \
    --request PUT \
    --data @payload.json \
    http://127.0.0.1:8200/v1/sys/license
```
