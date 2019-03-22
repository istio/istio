{{/* affinity - https://kubernetes.io/docs/concepts/configuration/assign-pod-node/ */}}

{{- define "gatewaynodeaffinity" }}
  nodeAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
    {{- include "gatewayNodeAffinityRequiredDuringScheduling" . }}
    preferredDuringSchedulingIgnoredDuringExecution:
    {{- include "gatewayNodeAffinityPreferredDuringScheduling" . }}
{{- end }}

{{- define "gatewayNodeAffinityRequiredDuringScheduling" }}
      nodeSelectorTerms:
      - matchExpressions:
        - key: beta.kubernetes.io/arch
          operator: In
          values:
        {{- range $key, $val := .root.Values.global.arch }}
          {{- if gt ($val | int) 0 }}
          - {{ $key }}
          {{- end }}
        {{- end }}
        {{- $nodeSelector := default .root.Values.global.defaultNodeSelector .nodeSelector -}}
        {{- range $key, $val := $nodeSelector }}
        - key: {{ $key }}
          operator: In
          values:
          - {{ $val }}
        {{- end }}
{{- end }}

{{- define "gatewayNodeAffinityPreferredDuringScheduling" }}
  {{- range $key, $val := .root.Values.global.arch }}
    {{- if gt ($val | int) 0 }}
    - weight: {{ $val | int }}
      preference:
        matchExpressions:
        - key: beta.kubernetes.io/arch
          operator: In
          values:
          - {{ $key }}
    {{- end }}
  {{- end }}
{{- end }}

{{- define "gatewaypodAntiAffinity" }}
{{- if or .podAntiAffinityLabelSelector .podAffinityTermLabelSelector}}
  podAntiAffinity:
    {{- if .podAntiAffinityLabelSelector }}
    requiredDuringSchedulingIgnoredDuringExecution:
    {{- include "gatewaypodAntiAffinityRequiredDuringScheduling" . }}
    {{- end }}
    {{- if or .podAffinityTermLabelSelector}}
    preferredDuringSchedulingIgnoredDuringExecution:
    {{- include "gatewaypodAntiAffinityPreferredDuringScheduling" . }}
    {{- end }}
{{- end }}
{{- end }}

{{- define "gatewaypodAntiAffinityRequiredDuringScheduling" }}
  {{- range $key, $val := .podAntiAffinityLabelSelector }}
    - labelSelector:
        matchExpressions:
        - key: {{ $key }}
          operator: In
          values:
          - {{ $val }}
      topologyKey: "kubernetes.io/hostname"
    - labelSelector:
        matchExpressions:
        - key: {{ $key }}
          operator: In
          values:
          - {{ $val }}
      topologyKey: "failure-domain.beta.kubernetes.io/zone"
  {{- end }}
{{- end }}

{{- define "gatewaypodAntiAffinityPreferredDuringScheduling" }}
    - weight: 100
    podAffinityTerm:
    {{- range $key, $val := .podAffinityTermLabelSelector }}
      labelSelector:
        matchExpressions:
        - key: {{ $key }}
          operator: In
          values:
          - {{ $val }}
      topologyKey: failure-domain.beta.kubernetes.io/region
    {{- end }}
{{- end }}
