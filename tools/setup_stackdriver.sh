#!/bin/bash

set -e

usage() {
    cat <<EOF

Generate stackdriver.yaml file to enable stackdriver handler for istio.

usage: ${0} [OPTIONS]

The following flags are required.

       --project_id       Project Id of the GCP Project.
       --sink_id          Id of the sink. Ex: temp-gcs
       --sink_destination Destination Id of the sink. Ex: storage.googleapis.com/temp_gcs
       --log_filter      Filter on how to filter logs for the sink. Ex: severity >= DEFAULT
EOF
    exit 1
}

while [[ $# -gt 0 ]]; do
    case ${1} in
        --project_id)
            project_id="$2"
            shift
            ;;
        --sink_id)
            sink_id="$2"
            shift
            ;;
        --sink_destination)
            sink_destination="$2"
            shift
            ;;
        --log_filter)
            log_filter="$2"
            shift
            ;;
        *)
            usage
            ;;
    esac
    shift
done

# We check for non-empty for project_id as it's required to setup stackdriver for the project.
if [[ $project_id = '' ]] ; then
  echo "Project Id can't be empty. Please provide a valid project id."
  exit 1
fi


{
    cat <<EOF > stackdriver.yaml
apiVersion: "config.istio.io/v1alpha2"
kind: stackdriver
metadata:
  name: handler
  namespace: istio-system
spec:
  # We'll use the default value from the adapter, once per minute, so we don't need to supply a value.
  # pushInterval: 1m
  # Must be supplied for the stackdriver adapter to work
  project_id: "$project_id"
  # One of the following must be set; the preferred method is `appCredentials`, which corresponds to
  # Google Application Default Credentials. See:
  #    https://developers.google.com/identity/protocols/application-default-credentials
  # If none is provided we default to app credentials.
  # appCredentials:
  # apiKey:
  # serviceAccountPath:

  # Describes how to map Istio metrics into Stackdriver.
  # Note: most of this config is copied over from prometheus.yaml to keep our metrics consistent across backends
  metricInfo:
    requestcount.metric.istio-system:
      # Due to a bug in gogoproto deserialization, Enums in maps must be
      # specified by their integer value, not variant name. See
      # https://github.com/googleapis/googleapis/blob/master/google/api/metric.proto
      # MetricKind and ValueType for the values to provide.
      kind: 2 # DELTA
      value: 2 # INT64
    requestduration.metric.istio-system:
      kind: 2 # DELTA
      value: 5 # DISTRIBUTION
      buckets:
        explicit_buckets:
          bounds: [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10]
    requestsize.metric.istio-system:
      kind: 2 # DELTA
      value: 5 # DISTRIBUTION
      buckets:
        exponentialBuckets:
          numFiniteBuckets: 8
          scale: 1
          growthFactor: 10
    responsesize.metric.istio-system:
      kind: 2 # DELTA
      value: 5 # DISTRIBUTION
      buckets:
        exponentialBuckets:
          numFiniteBuckets: 8
          scale: 1
          growthFactor: 10

  # Describes how to map Istio logs into Stackdriver.
  logInfo:
    accesslog.logentry.istio-system:
      payloadTemplate: '{{or (.sourceIp) "-"}} - {{or (.sourceUser) "-"}} [{{or (.timestamp.Format "02/Jan/2006:15:04:05 -0700") "-"}}] "{{or (.method) "-"}} {{or (.url) "-"}} {{or (.protocol) "-"}}" {{or (.responseCode) "-"}} {{or (.responseSize) "-"}}'
      httpMapping:
        url: url
        status: responseCode
        requestSize: requestSize
        responseSize: responseSize
        latency: latency
        localIp: sourceIp
        remoteIp: destinationIp
        method: method
        userAgent: userAgent
        referer: referer
      labelNames:
      - sourceIp
      - destinationIp
      - sourceService
      - sourceUser
      - sourceNamespace
      - destinationIp
      - destinationService
      - destinationNamespace
      - apiClaims
      - apiKey
      - protocol
      - method
      - url
      - responseCode
      - responseSize
      - requestSize
      - latency
      - connectionMtls
      - userAgent
      - responseTimestamp
      - receivedBytes
      - sentBytes
      - referer
      sinkInfo:
        id: '$sink_id'
        destination: '$sink_destination'
        filter: '$log_filter'
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: stackdriver
  namespace: istio-system
spec:
  match: "true" # If omitted match is true.
  actions:
  - handler: handler.stackdriver
    instances:
    - requestcount.metric
    - requestduration.metric
    - requestsize.metric
    - responsesize.metric
    - accesslog.logentry
---
EOF
}
