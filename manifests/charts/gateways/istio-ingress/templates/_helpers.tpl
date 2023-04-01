{{- define "isList" -}}
{{- if or (eq (typeOf .) "[]interface {}") (eq (typeOf .) "[]map[string]interface {}") -}}
  true
{{- else -}}
  false
{{- end -}}
{{- end -}}
