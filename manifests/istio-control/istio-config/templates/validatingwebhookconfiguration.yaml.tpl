{{/*
This version of the validatingwebhookconfiguration is applied indirectly
by galley. This exists to support a smoother upgrade path from istio
Rversions < 1.4
*/}}
{{ define "validatingwebhookconfiguration.yaml.tpl" }}
{{- if .Values.global.istiod.enabled }}
apiVersion: admissionregistration.k8s.io/v1beta1
kind: ValidatingWebhookConfiguration
metadata:
  name: istio-galley
  labels:
    app: galley
    release: {{ .Release.Name }}
    istio: galley
webhooks:
{{- else }}
apiVersion: admissionregistration.k8s.io/v1beta1
kind: ValidatingWebhookConfiguration
metadata:
  name: istio-galley
  labels:
    app: galley
    release: {{ .Release.Name }}
    istio: galley
webhooks:
  - name: pilot.validation.istio.io
    clientConfig:
      service:
        name: istio-galley
        namespace: {{ .Release.Namespace }}
        path: "/admitpilot"
      caBundle: ""
    rules:
      - operations:
        - CREATE
        - UPDATE
        apiGroups:
        - config.istio.io
        apiVersions:
        - v1alpha2
        resources:
        - httpapispecs
        - httpapispecbindings
        - quotaspecs
        - quotaspecbindings
      - operations:
        - CREATE
        - UPDATE
        apiGroups:
        - rbac.istio.io
        - security.istio.io
        - authentication.istio.io
        - networking.istio.io
        apiVersions:
        - "*"
        resources:
        - "*"
    # Fail open until the validation webhook is ready. The webhook controller
    # will update this to `Fail` and patch in the `caBundle` when the webhook
    # endpoint is ready.
    failurePolicy: Ignore
    sideEffects: None
  - name: mixer.validation.istio.io
    clientConfig:
      service:
        name: istio-galley
        namespace: {{ .Release.Namespace }}
        path: "/admitmixer"
      caBundle: ""
    rules:
      - operations:
        - CREATE
        - UPDATE
        apiGroups:
        - config.istio.io
        apiVersions:
        - v1alpha2
        resources:
        - rules
        - attributemanifests
        - adapters
        - handlers
        - instances
        - templates
    # Fail open until the validation webhook is ready. The webhook controller
    # will update this to `Fail` and patch in the `caBundle` when the webhook
    # endpoint is ready.
    failurePolicy: Ignore
    sideEffects: None
{{- end }}
{{- end }}
---
