---
title: Writing Configuration
headline: Writing Configuration
sidenav: doc-side-concepts-nav.html
bodyclass: docs
layout: docs
type: markdown
---

{% capture overview %}
This document describes how to write configuration that conforms to Istio's schemas. All configuration schemas in Istio are defined as [protobuf messages](https://developers.google.com/protocol-buffers/docs/proto3). When in doubt, search for the protos.
{% endcapture %}

{% capture body %}

## Translating to YAML
There is no canonical mapping between protobufs and YAML; instead [protobuf defines a canonical mapping to JSON](https://developers.google.com/protocol-buffers/docs/proto3#json), and [YAML defines a canonical mapping to JSON](http://yaml.org/spec/1.2/spec.html#id2759572). To ingest YAML as a proto we convert it to JSON then to  protobuf.

**Important things to note:**
- YAML fields are implicitly strings
- Proto `repeated` fields map to YAML lists; each element in a YAML list is prefixed by a dash (`-`)
- Proto `message`s map to JSON objects; in YAML objects are field names all at the same indentation level
- YAML is whitespace sensitive and must use spaces; tabs are never allowed

### `map` and `message` fields

<table>
  <tbody>
  <tr>
    <th>Proto</th>
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

### `repeated` fields

<table>
  <tbody>
  <tr>
    <th>Proto</th>
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

### `enum` fields

<table>
  <tbody>
    <tr>
      <th>Proto</th>
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
name: request.duration
value_type: INT64
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

*Note that YAML parsing will handle both `snake_case` and `lowerCamelCase` field names. `lowerCamelCase` is the canonical version in YAML.*

### Nested `message` fields

<table>
  <tbody>
    <tr>
      <th>Proto</th>
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