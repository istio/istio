# Amazon CloudWatch Adapter

This is an Istio Mixer adapter for integration with Amazon CloudWatch. Currently it supports sending metrics to CloudWatch.

## Configuration

To push metrics to CloudWatch using the adapter you need create an IAM user that has permissions to call cloudwatch APIs. The credentials for the user need to be available on the instance the adapter is running on (see [AWS docs](https://docs.aws.amazon.com/sdk-for-java/v1/developer-guide/setup-credentials.html)).

To activate the CloudWatch adapter, operators need to provide configuration for the
[cloudwatch adapter](https://istio.io/docs/reference/config/adapters/cloudwatch.html).

The handler configuration must contain the same metrics as the instance configuration. The metrics specified in both instance and handler configurations will be sent to CloudWatch.

## Example configuration

```yaml
# instance configuration for template 'metric'
apiVersion: "config.istio.io/v1alpha2"
kind: metric
metadata:
 name: cloudwatchrequestcount
 namespace: istio-system
spec:
 value: "1"
 dimensions:
   target: destination.service | "unknown"
   service: destination.labels["app"] | "unknown"
   response_code: response.code | 200
   time: request.time
---
# handler configuration for adapter 'metric'
apiVersion: "config.istio.io/v1alpha2"
kind: cloudwatch
metadata:
 name: hndlrTest
 namespace: istio-system
spec:
 namespace: "mixer-CloudWatch"
 metricInfo:
   cloudwatchrequestcount.metric.istio-system:
     unit: Count
     storageResolution: 1 #high resolution metric
---
# rule to dispatch to your handler
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
 name: mysamplerule
 namespace: istio-system
spec:
 match: "true"
 actions:
 - handler: hndlrTest.cloudwatch
   instances:
   - cloudwatchrequestcount.metric
```
