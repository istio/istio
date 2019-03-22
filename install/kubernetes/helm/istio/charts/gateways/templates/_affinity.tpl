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

{{- define "gatewayTolerations" }}
{{- $tolerations := default .root.Values.global.defaultTolerations .tolerations -}}
{{- range $index, $item := $tolerations }}
- key: {{ $item.key }}
  operator: {{ $item.operator }}
  {{- if $item.value }}
  value: {{ $item.value }}
  {{- end }}
  effect: {{ $item.effect }}
{{- end }}
{{- end }}
