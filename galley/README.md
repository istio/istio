# Galley

Galley is the top-level config ingestion, processing and distribution component of
Istio. It is responsible for insulating the rest of the Istio components from the
details of obtaining user configuration from the underlying platform. It contains 
Kubernetes CRD listeners for collecting configuration, an MCP protocol server
implementation for distributing config, and a validation web-hook for pre-ingestion 
validation by Kubernetes API Server.
