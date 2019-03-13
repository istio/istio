{{/* affinity - https://kubernetes.io/docs/concepts/configuration/assign-pod-node/ */}}

{{- define "nodeaffinity" }}
  nodeAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
    {{- include "nodeAffinityRequiredDuringScheduling" . }}
    preferredDuringSchedulingIgnoredDuringExecution:
    {{- include "nodeAffinityPreferredDuringScheduling" . }}
{{- end }}

{{- define "nodeAffinityRequiredDuringScheduling" }}
      nodeSelectorTerms:
      - matchExpressions:
        - key: beta.kubernetes.io/arch
          operator: In
          values:
        {{- range $key, $val := .Values.global.arch }}
          {{- if gt ($val | int) 0 }}
          - {{ $key }}
          {{- end }}
        {{- end }}
        {{- $nodeSelector := default .Values.global.defaultNodeSelector .Values.nodeSelector -}}
        {{- range $key, $val := $nodeSelector }}
        - key: {{ $key }}
          operator: In
          values:
          - {{ $val }}
        {{- end }}
{{- end }}

{{- define "nodeAffinityPreferredDuringScheduling" }}
  {{- range $key, $val := .Values.global.arch }}
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

{{- define "podAntiAffinity" }}
{{- if or .Values.podAntiAffinityLabelSelector .Values.podAffinityTermLabelSelector}}
  podAntiAffinity:
    {{- if .Values.podAntiAffinityLabelSelector }}
    requiredDuringSchedulingIgnoredDuringExecution:
    {{- include "podAntiAffinityRequiredDuringScheduling" . }}
    {{- end }}
    {{- if or .Values.podAffinityTermLabelSelector}}
    preferredDuringSchedulingIgnoredDuringExecution:
    {{- include "podAntiAffinityPreferredDuringScheduling" . }}
    {{- end }}
{{- end }}
{{- end }}

{{- define "podAntiAffinityRequiredDuringScheduling" }}
      {{- range $key, $val := .Values.podAntiAffinityLabelSelector }}
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

{{- define "podAntiAffinityPreferredDuringScheduling" }}
      - weight: 100
      podAffinityTerm:
      {{- range $key, $val := .Values.podAffinityTermLabelSelector }}
      - labelSelector:
        matchExpressions:
        - key: {{ $key }}
          operator: In
          values:
          - {{ $val }}
        topologyKey: failure-domain.beta.kubernetes.io/region
        {{- end }}
{{- end }}
