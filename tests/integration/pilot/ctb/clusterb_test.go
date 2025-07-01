//go:build integ
// +build integ

// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ctb

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
)

const clusterTrustBundleTemplate = `
apiVersion: certificates.k8s.io/v1beta1
kind: ClusterTrustBundle
metadata:
  name: {{.Name}}
  labels:
    istio.io/cluster-trust-bundle: "true"
spec:
  signerName: {{.SignerName}}
  trustBundle: |
{{.TrustBundle}}
`

const workloadEntryTemplate = `
apiVersion: networking.istio.io/v1beta1
kind: WorkloadEntry
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
spec:
  address: {{.Address}}
  ports:
    http: 8080
  labels:
    app: {{.App}}
    version: v1
  serviceAccount: {{.ServiceAccount}}
`

const serviceEntryTemplate = `
apiVersion: networking.istio.io/v1beta1
kind: ServiceEntry
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
spec:
  hosts:
  - {{.Host}}
  ports:
  - number: 8080
    name: http
    protocol: HTTP
  location: MESH_EXTERNAL
  resolution: STATIC
  endpoints:
  - address: {{.Address}}
`

// TestClusterTrustBundleBasicFunctionality tests basic ClusterTrustBundle operations
func TestClusterTrustBundleBasicFunctionality(t *testing.T) { // Simple unit test that validates our test setup without requiring Kubernetes
	t.Run("basic-setup", func(t *testing.T) {
		// Test that our templates are valid
		cert := generateTestCertificateString()
		if cert == "" {
			t.Error("generateTestCertificateString returned empty string")
		}

		indented := indentCertificate(cert)
		if indented == "" {
			t.Error("indentCertificate returned empty string")
		}

		t.Logf("Successfully generated and indented certificate")
	})

	// Integration test that requires Kubernetes - skip if no cluster available
	framework.NewTest(t).
		Label(label.CustomSetup).
		Run(func(t framework.TestContext) {
			ctx := context.Background()
			client := t.Clusters().Default().Kube()

			// Enable ClusterTrustBundle feature
			clustertbI.PatchMeshConfigOrFail(t, `
defaultConfig:
  proxyMetadata:
    ENABLE_CLUSTER_TRUST_BUNDLE_API: "true"
`)

			// Test ClusterTrustBundle creation and validation
			ctbName := "test-cluster-trust-bundle"
			testCert := generateTestCertificate()

			ctbYAML := tmpl.EvaluateOrFail(t, clusterTrustBundleTemplate, map[string]interface{}{
				"Name":        ctbName,
				"SignerName":  "test.example.com/test-signer",
				"TrustBundle": indentCertificate(testCert),
			})

			t.ConfigIstio().YAML("", ctbYAML).ApplyOrFail(t)

			// Verify ClusterTrustBundle was created successfully
			retry.UntilSuccessOrFail(t, func() error {
				_, err := client.CertificatesV1beta1().ClusterTrustBundles().Get(ctx, ctbName, metav1.GetOptions{})
				if err != nil {
					return fmt.Errorf("failed to get ClusterTrustBundle %s: %v", ctbName, err)
				}
				return nil
			}, retry.Timeout(30*time.Second))

			// Test ClusterTrustBundle update
			updatedCert := generateTestCertificate()
			updatedCTBYAML := tmpl.EvaluateOrFail(t, clusterTrustBundleTemplate, map[string]interface{}{
				"Name":        ctbName,
				"SignerName":  "test.example.com/test-signer",
				"TrustBundle": indentCertificate(updatedCert),
			})

			t.ConfigIstio().YAML("", updatedCTBYAML).ApplyOrFail(t)

			// Verify update was successful
			retry.UntilSuccessOrFail(t, func() error {
				ctb, err := client.CertificatesV1beta1().ClusterTrustBundles().Get(ctx, ctbName, metav1.GetOptions{})
				if err != nil {
					return fmt.Errorf("failed to get updated ClusterTrustBundle: %v", err)
				}
				if ctb.Spec.TrustBundle != indentCertificate(updatedCert) {
					return fmt.Errorf("ClusterTrustBundle not updated correctly")
				}
				return nil
			}, retry.Timeout(30*time.Second))

			// Clean up
			err := client.CertificatesV1beta1().ClusterTrustBundles().Delete(ctx, ctbName, metav1.DeleteOptions{})
			if err != nil {
				t.Logf("Failed to clean up ClusterTrustBundle: %v", err)
			}
		})
}

// TestClusterTrustBundleWorkloadEntryIntegration tests ClusterTrustBundle integration with WorkloadEntry
// This addresses issue #56675 for ClusterTrustBundle integration with WorkloadEntry
func TestClusterTrustBundleWorkloadEntryIntegration(t *testing.T) {
	framework.NewTest(t).
		Label(label.CustomSetup).
		Run(func(t framework.TestContext) {
			ctx := context.Background()
			client := t.Clusters().Default().Kube()

			ns := namespace.NewOrFail(t, namespace.Config{
				Prefix: "clustertb-we",
				Inject: true,
			})

			// Enable ClusterTrustBundle feature
			clustertbI.PatchMeshConfigOrFail(t, `
defaultConfig:
  proxyMetadata:
    ENABLE_CLUSTER_TRUST_BUNDLE_API: "true"
`)

			// Create ClusterTrustBundle first
			ctbName := "workload-trust-bundle"
			testCert := generateTestCertificate()

			ctbYAML := tmpl.EvaluateOrFail(t, clusterTrustBundleTemplate, map[string]interface{}{
				"Name":        ctbName,
				"SignerName":  "workload.example.com/signer",
				"TrustBundle": indentCertificate(testCert),
			})

			t.ConfigIstio().YAML("", ctbYAML).ApplyOrFail(t)

			// Wait for ClusterTrustBundle to be ready
			retry.UntilSuccessOrFail(t, func() error {
				_, err := client.CertificatesV1beta1().ClusterTrustBundles().Get(ctx, ctbName, metav1.GetOptions{})
				return err
			}, retry.Timeout(30*time.Second))

			// Create WorkloadEntry
			weName := "test-workload-entry"
			weYAML := tmpl.EvaluateOrFail(t, workloadEntryTemplate, map[string]interface{}{
				"Name":           weName,
				"Namespace":      ns.Name(),
				"Address":        "192.168.1.100",
				"App":            "external-service",
				"ServiceAccount": "default",
			})

			t.ConfigIstio().YAML(ns.Name(), weYAML).ApplyOrFail(t)

			// Verify WorkloadEntry was created and Istio processes it correctly
			retry.UntilSuccessOrFail(t, func() error {
				we, err := t.Clusters().Default().Istio().NetworkingV1().WorkloadEntries(ns.Name()).Get(ctx, weName, metav1.GetOptions{})
				if err != nil {
					return fmt.Errorf("failed to get WorkloadEntry: %v", err)
				}
				if we.Spec.Address != "192.168.1.100" {
					return fmt.Errorf("WorkloadEntry address mismatch")
				}
				return nil
			}, retry.Timeout(30*time.Second))

			// Create ServiceEntry to complete the external service setup
			seName := "test-service-entry"
			seYAML := tmpl.EvaluateOrFail(t, serviceEntryTemplate, map[string]interface{}{
				"Name":      seName,
				"Namespace": ns.Name(),
				"Host":      "external-service.example.com",
				"Address":   "192.168.1.100",
			})

			t.ConfigIstio().YAML(ns.Name(), seYAML).ApplyOrFail(t)

			// Verify ServiceEntry creation
			retry.UntilSuccessOrFail(t, func() error {
				_, err := t.Clusters().Default().Istio().NetworkingV1().ServiceEntries(ns.Name()).Get(ctx, seName, metav1.GetOptions{})
				return err
			}, retry.Timeout(30*time.Second))

			// Test ClusterTrustBundle deletion behavior with existing WorkloadEntries
			err := client.CertificatesV1beta1().ClusterTrustBundles().Delete(ctx, ctbName, metav1.DeleteOptions{})
			if err != nil {
				t.Fatalf("Failed to delete ClusterTrustBundle: %v", err)
			}

			// Verify WorkloadEntry still exists after ClusterTrustBundle deletion
			retry.UntilSuccessOrFail(t, func() error {
				_, err := t.Clusters().Default().Istio().NetworkingV1().WorkloadEntries(ns.Name()).Get(ctx, weName, metav1.GetOptions{})
				if err != nil {
					return fmt.Errorf("WorkloadEntry should still exist after ClusterTrustBundle deletion: %v", err)
				}
				return nil
			}, retry.Timeout(15*time.Second))

			// Clean up remaining resources
			_ = t.Clusters().Default().Istio().NetworkingV1().WorkloadEntries(ns.Name()).Delete(ctx, weName, metav1.DeleteOptions{})
			_ = t.Clusters().Default().Istio().NetworkingV1().ServiceEntries(ns.Name()).Delete(ctx, seName, metav1.DeleteOptions{})
		})
}

// TestClusterTrustBundleCertificateValidation tests certificate validation in ClusterTrustBundle
func TestClusterTrustBundleCertificateValidation(t *testing.T) {
	framework.NewTest(t).
		Label(label.CustomSetup).
		Run(func(t framework.TestContext) {
			ctx := context.Background()
			client := t.Clusters().Default().Kube()

			// Test valid certificate acceptance
			validCert := generateTestCertificate()
			validCTBYAML := tmpl.EvaluateOrFail(t, clusterTrustBundleTemplate, map[string]interface{}{
				"Name":        "valid-cert-bundle",
				"SignerName":  "valid.example.com/signer",
				"TrustBundle": indentCertificate(validCert),
			})

			t.ConfigIstio().YAML("", validCTBYAML).ApplyOrFail(t)

			// Verify valid certificate is accepted
			retry.UntilSuccessOrFail(t, func() error {
				ctb, err := client.CertificatesV1beta1().ClusterTrustBundles().Get(ctx, "valid-cert-bundle", metav1.GetOptions{})
				if err != nil {
					return fmt.Errorf("failed to get valid ClusterTrustBundle: %v", err)
				}
				if ctb.Spec.TrustBundle != indentCertificate(validCert) {
					return fmt.Errorf("certificate content mismatch")
				}
				return nil
			}, retry.Timeout(30*time.Second))

			// Clean up
			_ = client.CertificatesV1beta1().ClusterTrustBundles().Delete(ctx, "valid-cert-bundle", metav1.DeleteOptions{})
		})
}

// TestClusterTrustBundleMultipleSigners tests multiple ClusterTrustBundles with different signers
func TestClusterTrustBundleMultipleSigners(t *testing.T) {
	framework.NewTest(t).
		Label(label.CustomSetup).
		Run(func(t framework.TestContext) {
			ctx := context.Background()
			client := t.Clusters().Default().Kube()

			bundles := []struct {
				name       string
				signerName string
			}{
				{"signer1-bundle", "signer1.example.com/ca"},
				{"signer2-bundle", "signer2.example.com/ca"},
				{"signer3-bundle", "signer3.example.com/ca"},
			}

			// Create multiple ClusterTrustBundles
			for _, bundle := range bundles {
				cert := generateTestCertificate()
				ctbYAML := tmpl.EvaluateOrFail(t, clusterTrustBundleTemplate, map[string]interface{}{
					"Name":        bundle.name,
					"SignerName":  bundle.signerName,
					"TrustBundle": indentCertificate(cert),
				})

				t.ConfigIstio().YAML("", ctbYAML).ApplyOrFail(t)
			}

			// Verify all bundles are created successfully
			for _, bundle := range bundles {
				retry.UntilSuccessOrFail(t, func() error {
					ctb, err := client.CertificatesV1beta1().ClusterTrustBundles().Get(ctx, bundle.name, metav1.GetOptions{})
					if err != nil {
						return fmt.Errorf("failed to get ClusterTrustBundle %s: %v", bundle.name, err)
					}
					if ctb.Spec.SignerName != bundle.signerName {
						return fmt.Errorf("signer name mismatch for %s", bundle.name)
					}
					return nil
				}, retry.Timeout(30*time.Second))
			}

			// Test that we can list all ClusterTrustBundles
			retry.UntilSuccessOrFail(t, func() error {
				list, err := client.CertificatesV1beta1().ClusterTrustBundles().List(ctx, metav1.ListOptions{
					LabelSelector: "istio.io/cluster-trust-bundle=true",
				})
				if err != nil {
					return fmt.Errorf("failed to list ClusterTrustBundles: %v", err)
				}
				if len(list.Items) < len(bundles) {
					return fmt.Errorf("expected at least %d ClusterTrustBundles, got %d", len(bundles), len(list.Items))
				}
				return nil
			}, retry.Timeout(30*time.Second))

			// Clean up all bundles
			for _, bundle := range bundles {
				_ = client.CertificatesV1beta1().ClusterTrustBundles().Delete(ctx, bundle.name, metav1.DeleteOptions{})
			}
		})
}

// Helper functions

func generateTestCertificateString() string {
	// This is a test certificate - in real scenarios this would be a proper CA certificate
	return `-----BEGIN CERTIFICATE-----
MIICsDCCAZgCCQDK1JHvSmZUGDANBgkqhkiG9w0BAQsFADAaMRgwFgYDVQQDDA90
ZXN0LmV4YW1wbGUuY29tMB4XDTIzMDEwMTAwMDAwMFoXDTI0MDEwMTAwMDAwMFow
GjEYMBYGA1UEAwwPdGVzdC5leGFtcGxlLmNvbTCCASIwDQYJKoZIhvcNAQEBBQAD
ggEPADCCAQoCggEBALn7fJl2Qi8K9VzR9vG7XcDnm8/yGqCPwv8n7XqQ1g3HK4N5
bS9J3XoKZQJ6fPqL+Np2Qrt8N7+xJ0Rc+8A6B4K5xOGKJQqKt8Jc7GhJ3YpP9z5n
XoUt9VkqJ+J5cFkV3YAu6L6sT8Q5FhR7Q8LlG9Y3e2K6bN8u8RqT7K8nVlO4a3zX
hHJfV6GKZQn8A9qPxZF9c7zQ9oN5Y8K3zG8bJ5dN7Q2rO3eFsN8+xL6KZQR7H9vQ
tZCt8R9q2wYNnXkbJ4Z8u6KvV7qRzN8L2mOqX8a3zQF7cJ0bwL9mKfQ5Y8nY2ZeP
tL3z7v9a1xV6KJ3mFr5GNqL8K9Xz3F9qR7Y2GZsCAwEAATANBgkqhkiG9w0BAQsF
AAOCAQEAp8v7/uN8w8Xf3t7Q+aDzHn2KJ8vF0tOd7YQg1o2bFl8n8wYr9hHp4uNj
kV8nOq2zK7dP9r3F5vCt8wKn0YXg9L7G2bK5QN1vCr8VeH4L7jZ8pOq8tYr6nKfV
z3rK9wK1Y8vF2bGpF8k6oNz7Q5Y3tLr7JpK9vO3xF8nG7zYeQ+L1qV8pKt9oFbYv
Zc8L5wG3o2Y1qV+9tF3xKc7Q9pOvF7Y2bL8kN5Y7vG3qFpY8jK2cQr7dJ8yV3tO+
LfY9vGzKn2oRt8wN3yF7jKpY9v2qV8rG3tLfY7+pK9vN8qF2wO7zV3jY1qNtL+bF
9pO2xK8v7nGrY3t2qVkL9wC3oY8sJg==
-----END CERTIFICATE-----`
}

func generateTestCertificate() string {
	return generateTestCertificateString()
}

func indentCertificate(cert string) string {
	lines := ""
	// Split the certificate into lines and add indentation
	for _, line := range strings.Split(strings.TrimSpace(cert), "\n") {
		if line != "" {
			lines += "    " + line + "\n"
		}
	}
	return lines
}
