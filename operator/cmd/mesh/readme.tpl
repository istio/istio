# IstioOperator to Helm Migration

This folder contains auto-generated output from the `istioctl manifest translate` command.
Note the `manifest translate` command only outputs this folders contents, and does not modify the cluster state.

Follow the instructions below for each component to complete the migration.

# Components
{{- range $c := .Results }}
{{$c}}
{{- end}}
