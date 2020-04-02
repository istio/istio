{{ define "validatingwebhookconfiguration.yaml.tpl" }}
apiVersion: admissionregistration.k8s.io/v1beta1
kind: ValidatingWebhookConfiguration
metadata:
  name: istio-galley
  labels:
    app: galley
    release: {{ .Release.Name }}
    istio: galley
webhooks:
{{- end }}
---
