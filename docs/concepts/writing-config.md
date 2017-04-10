---
title: Writing Istio Configuration
headline: Writing Istio Configuration
sidenav: doc-side-concepts-nav.html
bodyclass: docs
layout: docs
type: markdown
---

{% capture overview %}
This document describes how to write configuration that conforms to Istio's schemas. All configuration schemas in Istio are defined as protobuf messages. When in doubt, search for the protos.
{% endcapture %}

{% capture body %}
## Translating to JSON
The protobuf language guide defines [the canonical mapping between protobuf and JSON](https://developers.google.com/protocol-buffers/docs/proto3#json), refer to it as the source of truth. We provide a few specific examples that appear in the Mixer's configuration.

### Proto `map` and `message` translate to JSON objects:

<table>
  <tbody>
  <tr>
    <th>Proto</th>
    <th>JSON</th>
  </tr>
  <tr>
    <td>
<pre>
message Metric {
        string descriptor_name = 1;
        string value = 2;
        map<string, string> labels = 3;
}
</pre>
    </td>
    <td>
<pre>
{
  "descriptorName": "request_count",
  "value": "1",
  "labels": {
      "source": "origin.ip",
        "target": "target.service",
  },
}  
</pre>
    </td>
  </tr>
</tbody>
</table>

*Note that a map's keys are always converted to strings, no matter the data type declared in the proto file. They are converted back to the correct runtime type by the proto parsing libraries.*

### Proto `repeated` fields translate to JSON arrays:

<table>
  <tbody>
  <tr>
    <th>Proto</th>
    <th>JSON</th>
  </tr>
  <tr>
    <td>
<pre>`
message Metric {
        string descriptor_name = 1;
        string value = 2;
        map<string, string> labels = 3;
}

message MetricsParams {
    repeated Metric metrics = 1;
}
</pre>
    </td>
    <td>
<pre>
{
  "metrics": [
    {
      "descriptorName": "request_count",
      "value": "1",
      "labels": {
            "source": "origin.ip",
            "target": "target.service",
      },
    }, {
      "descriptorName": "request_latency",
      "value": "response.duration",
      "labels": {
            "source": "origin.ip",
            "target": "target.service",
      },
    }
  ]
}  
</pre>
    </td>
  </tr>
</tbody>
</table>

*Note that we never name the outer-most message (the `MetricsParams`), but we do need the outer set of braces marking our JSON as an object. Also note that we shift from snake_case to lowerCamelCase; both are accepted but lowerCamelCase is idiomatic.*

### Proto `enum` fields uses the name of the value:

<table>
  <tbody>
    <tr>
      <th>Proto</th>
      <th>JSON</th>
    </tr>
    <tr>
      <td>
<pre>
enum ValueType {
    STRING = 1;
    INT64 = 2;
    DOUBLE = 3;
    // more values omitted
}

message AttributeDescriptor {
    string name = 1;
    string description = 2;
    ValueType value_type = 3;
}
</pre>
      </td>
      <td>
<pre>
{
  "name": "request.duration",
  "valueType": "INT64",
}
</pre>
      </td>
    </tr>
  </tbody>
</table>

*Notice we don't provide a `description`: fields with no value can be omitted.*

### Proto `message` fields can contain other messages:
<table>
  <tbody>
    <tr>
      <th>Proto</th>
      <th>JSON</th>
    </tr>
    <tr>
      <td>
<pre>
enum ValueType {
    STRING = 1;
    INT64 = 2;
    DOUBLE = 3;
    // more values omitted
}

message LabelDescriptor {
    string name = 1;
    string description = 2;
    ValueType value_type = 3;
}

message MonitoredResourceDescriptor {
  string name = 1;
  string description = 2;
  repeated LabelDescriptor labels = 3;
}
</pre>
      </td>
      <td>
<pre>
{
  "name": "My Monitored Resource",
  "labels": [
    {
      "name": "label one",
      "valueType": "STRING",
    }, {
      "name": "second label",
      "valueType": "DOUBLE",
    },
  ],
}
</pre>
      </td>
    </tr>
  </tbody>
</table>


## Translating to YAML

There is no canonical mapping between protobufs and YAML; instead protobuf defines a canonical mapping to JSON, and YAML defines a canonical mapping to JSON. To ingest YAML as a proto we convert it to JSON then to  protobuf.

**Important things to note:**
- YAML fields are implicitly strings
- JSON arrays map to YAML lists; each element in a list is prefixed by a dash (`-`)
- JSON objects map to a set of field names all at the same indentation level in YAML
- YAML is whitespace sensitive and must use spaces; tabs are never allowed

### Proto `map` and `message` fields:
<table>
  <tbody>
  <tr>
    <th>Proto</th>
    <th>JSON</th>
    <th>YAML</th>
  </tr>
  <tr>
    <td>
<pre>
message Metric {
        string descriptor_name = 1;
        string value = 2;
        map<string, string> labels = 3;
}
</pre>
    </td>
    <td>
<pre>
{
  "descriptorName": "request_count",
  "value": "1",
  "labels": {
      "source": "origin.ip",
      "target": "target.service",
  },
}  
</pre>
    </td>
    <td>
<pre>
descriptorName: request_count
value: "1"
labels:
  source: origin.ip
  target: target.service
</pre>
    </td>
  </tr>
</tbody>
</table>

*Note that when numeric literals are used as strings (like `value` above) they must be enclosed in quotes. Quotation marks (`"`) are optional for normal strings.*

### Proto `repeated` fields:
<table>
  <tbody>
  <tr>
    <th>Proto</th>
    <th>JSON</th>
    <th>YAML</th>
  </tr>
  <tr>
    <td>
<pre>
message Metric {
        string descriptor_name = 1;
        string value = 2;
        map<string, string> labels = 3;
}

message MetricsParams {
    repeated Metric metrics = 1;
}
</pre>
    </td>
    <td>
<pre>
{
  "metrics": [
    {
      "descriptorName": "request_count",
      "value": "1",
      "labels": {
            "source": "origin.ip",
            "target": "target.service",
      },
    }, {
      "descriptorName": "request_latency",
      "value": "response.duration",
      "labels": {
            "source": "origin.ip",
            "target": "target.service",
      },
    }
  ]
}  
</pre>
    </td>
    <td>
<pre>
metrics:
- descriptorName: request_count
  value: "1"
  labels:
    source: origin.ip
    target: target.service
- descriptorName: request_latency
  value: response.duration
  labels:
    source: origin.ip
    target: target.service
</pre>
    </td>
  </tr>
</tbody>
</table>

### Proto `enum` fields:
<table>
  <tbody>
    <tr>
      <th>Proto</th>
      <th>JSON</th>
      <th>YAML</th>
    </tr>
    <tr>
      <td>
<pre>
enum ValueType {
    STRING = 1;
    INT64 = 2;
    DOUBLE = 3;
    // more values omitted
}

message AttributeDescriptor {
    string name = 1;
    string description = 2;
    ValueType value_type = 3;
}
</pre>
      </td>
      <td>
<pre>
{
  "name": "request.duration",
  "valueType": "INT64",
}
</pre>
      </td>
      <td>
<pre>
name: request.duration
valueType: INT64
</pre>

or

<pre>
name: request.duration
valueType: INT64
</pre>
      </td>
    </tr>
  </tbody>
</table>

*Note that YAML parsing will handle both `snake_case` and `camelCase` field names.*

### Proto with nested `message` fields:
<table>
  <tbody>
    <tr>
      <th>Proto</th>
      <th>JSON</th>
      <th>YAML</th>
    </tr>
    <tr>
      <td>
<pre>
enum ValueType {
    STRING = 1;
    INT64 = 2;
    DOUBLE = 3;
    // more values omitted
}

message LabelDescriptor {
    string name = 1;
    string description = 2;
    ValueType value_type = 3;
}

message MonitoredResourceDescriptor {
  string name = 1;
  string description = 2;
  repeated LabelDescriptor labels = 3;
}
</pre>
      </td>
      <td>
<pre>
{
  "name": "My Monitored Resource",
  "labels": [
    {
      "name": "label one",
      "valueType": "STRING",
    }, {
      "name": "second label",
      "valueType": "DOUBLE",
    },
  ],
}
</pre>
      </td>
      <td>
<pre>
name: My Monitored Resource
labels:
- name: label one
  valueType: STRING
- name: second label
  valueType: DOUBLE
</pre>
      </td>
    </tr>
  </tbody>
</table>

{% endcapture %}

{% capture whatsnext %}
* TODO: link to overall mixer config concept guide (how the config pieces fit together)
{% endcapture %}

{% include templates/concept.md %}