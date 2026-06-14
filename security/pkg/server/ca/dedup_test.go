package ca

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"

	pb "istio.io/api/security/v1alpha1"
	"istio.io/istio/pkg/security"
	mockca "istio.io/istio/security/pkg/pki/ca/mock"
	"istio.io/istio/security/pkg/pki/util"
)

// ---- test helpers -----------------------------------------------------------

// mkCert returns a self-signed ECDSA P-256 cert as a PEM string. Cert is
// CA-marked for simplicity; only DER identity is what these tests assert.
func mkCert(t *testing.T, cn string) string {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key gen: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func concat(parts ...string) string { return strings.Join(parts, "") }

// countCerts counts CERTIFICATE PEM blocks across a chain slice.
func countCerts(chain []string) int {
	n := 0
	for _, e := range chain {
		rest := []byte(e)
		for {
			b, r := pem.Decode(rest)
			if b == nil {
				break
			}
			if b.Type == "CERTIFICATE" {
				n++
			}
			rest = r
		}
	}
	return n
}

// fpsOfChain returns SHA-256 hex fingerprints of every CERTIFICATE block in
// the chain (in order).
func fpsOfChain(t *testing.T, chain []string) []string {
	t.Helper()
	var fps []string
	for _, e := range chain {
		rest := []byte(e)
		for {
			b, r := pem.Decode(rest)
			if b == nil {
				break
			}
			if b.Type == "CERTIFICATE" {
				h := sha256.Sum256(b.Bytes)
				fps = append(fps, hex.EncodeToString(h[:]))
			}
			rest = r
		}
	}
	return fps
}

func fpOf(t *testing.T, pemCert string) string {
	t.Helper()
	b, _ := pem.Decode([]byte(pemCert))
	if b == nil {
		t.Fatalf("fpOf: invalid PEM")
	}
	h := sha256.Sum256(b.Bytes)
	return hex.EncodeToString(h[:])
}

// ---- table-driven helper tests ----------------------------------------------

// TestDedupCertChain exercises the helper across every behaviour class that
// the design doc enumerates. The matrix collapses what was previously 16
// individual functions into one parameterised table.
func TestDedupCertChain(t *testing.T) {
	// Generate the certs ONCE so every row references the same SHA-256
	// identities and the table reads as a literal.
	leaf := mkCert(t, "leaf")
	inter := mkCert(t, "Intermediate CA")
	int2 := mkCert(t, "Intermediate CA 2")
	root := mkCert(t, "Root CA")
	rootOld := mkCert(t, "Root CA Old")
	rootNew := mkCert(t, "Root CA New")
	whitespaceVariant := root + "\n\n"
	nonCert := string(pem.EncodeToMemory(&pem.Block{Type: "FOO", Bytes: []byte{0x01, 0x02}}))

	cases := []struct {
		name string
		in   []string
		// wantCerts <0 means "do not check count" (caller uses extraChecks).
		wantCerts int
		// wantLastFP == "" means "do not check last element identity".
		wantLastFP string
		// extraChecks is an optional post-condition.
		extraChecks func(t *testing.T, out []string)
	}{
		// ---- defensive ------------------------------------------------------
		{name: "nil", in: nil, wantCerts: 0},
		{name: "empty", in: []string{}, wantCerts: 0},
		{name: "single-element passthrough", in: []string{leaf}, wantCerts: 1},

		// ---- core dedup behaviour -------------------------------------------
		{
			name: "canonical bug: leaf || inter+root || root",
			// Models server response BEFORE dedup using the Istio Makefile
			// cert-chain.pem (inter||root) plus the appended rootCertBytes.
			in:         []string{leaf, concat(inter, root), root},
			wantCerts:  3,
			wantLastFP: fpOf(t, root),
		},
		{
			name:       "all individual elements (after PemCertBytestoString expansion)",
			in:         []string{leaf, inter, root, root},
			wantCerts:  3,
			wantLastFP: fpOf(t, root),
		},
		{
			name:       "triple duplicate root",
			in:         []string{root, root, root},
			wantCerts:  1,
			wantLastFP: fpOf(t, root),
		},
		{
			name:       "duplicate intermediate (helper is general, not root-only)",
			in:         []string{leaf, inter, inter, root},
			wantCerts:  3,
			wantLastFP: fpOf(t, root),
		},
		{
			name:       "non-standard root-first cert-chain.pem layout",
			in:         []string{leaf, root, inter, root}, // root||inter then appended root
			wantCerts:  3,
			wantLastFP: fpOf(t, root),
		},

		// ---- no-change cases (fast-path) ------------------------------------
		{
			name:      "self-signed mode (no chain bytes)",
			in:        []string{leaf, root},
			wantCerts: 2,
		},
		{
			name:      "multi-intermediate hierarchy, no duplicate",
			in:        []string{leaf, concat(inter, int2), root},
			wantCerts: 4,
		},

		// ---- rotation -------------------------------------------------------
		{
			name: "rotation: trust bundle holds old||new roots (helper-level)",
			// rootCertBytes = old||new. cert-chain.pem references old, so old
			// appears once in chain bytes and once at head of trust bundle.
			in:        []string{leaf, concat(inter, rootOld), concat(rootOld, rootNew)},
			wantCerts: 4,
			extraChecks: func(t *testing.T, out []string) {
				flat := strings.Join(out, "")
				if !strings.Contains(flat, rootOld) {
					t.Errorf("rotation: old root must remain present (still signing)")
				}
				if !strings.Contains(flat, rootNew) {
					t.Errorf("rotation: new root must be present (clients need it)")
				}
			},
		},
		{
			name: "rotation: all-individual elements with multi-block trust bundle",
			// cert-chain.pem (inter||rootOld) → expanded into [inter, rootOld]
			// then rootCertBytes "rootOld\nrootNew" appended as one element.
			in:        []string{leaf, inter, rootOld, concat(rootOld, rootNew)},
			wantCerts: 4,
			extraChecks: func(t *testing.T, out []string) {
				last := out[len(out)-1]
				if !strings.Contains(last, rootOld) || !strings.Contains(last, rootNew) {
					t.Errorf("trust bundle must be the last element; last=%q", last)
				}
			},
		},

		// ---- edge cases -----------------------------------------------------
		{
			name:      "whitespace-only difference is treated as duplicate",
			in:        []string{root, whitespaceVariant},
			wantCerts: 1, // dedup is DER-based
		},
		{
			name:      "external certSigner: leaf||inter||root || root",
			in:        []string{concat(leaf, inter, root), root},
			wantCerts: 3,
		},
		{
			name:      "misconfigured: cert-chain.pem contains ONLY the root",
			in:        []string{leaf, root, root},
			wantCerts: 2,
		},
		{
			name:      "non-CERTIFICATE PEM blocks preserved",
			in:        []string{leaf, nonCert},
			wantCerts: -1,
			extraChecks: func(t *testing.T, out []string) {
				if !strings.Contains(strings.Join(out, ""), "BEGIN FOO") {
					t.Errorf("unknown PEM block was dropped: %v", out)
				}
			},
		},
		{
			name:      "pure non-PEM passthrough",
			in:        []string{"not pem at all"},
			wantCerts: -1,
			extraChecks: func(t *testing.T, out []string) {
				if len(out) != 1 || out[0] != "not pem at all" {
					t.Errorf("non-PEM passthrough broken: %v", out)
				}
			},
		},
		{
			name:      "empty string element preserved verbatim",
			in:        []string{leaf, "", root},
			wantCerts: -1,
			extraChecks: func(t *testing.T, out []string) {
				found := false
				for _, e := range out {
					if e == "" {
						found = true
					}
				}
				if !found {
					t.Errorf("empty string element was dropped: %v", out)
				}
				if got := countCerts(out); got != 2 {
					t.Errorf("expected leaf+root = 2 certs, got %d", got)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := dedupCertChain(tc.in)
			if tc.wantCerts >= 0 {
				if got := countCerts(out); got != tc.wantCerts {
					t.Fatalf("cert count: want=%d got=%d (out=%v)", tc.wantCerts, got, out)
				}
			}
			if tc.wantLastFP != "" {
				fps := fpsOfChain(t, out)
				if len(fps) == 0 || fps[len(fps)-1] != tc.wantLastFP {
					t.Errorf("last cert must have fp=%s; got %v", tc.wantLastFP, fps)
				}
			}
			if tc.extraChecks != nil {
				tc.extraChecks(t, out)
			}
		})
	}
}

// TestDedupCertChain_FastPathReturnsInputSlice asserts the zero-allocation
// fast-path: when no duplicates exist the helper must return the input slice
// unchanged (identity check) so well-configured clusters pay no rewrite cost.
func TestDedupCertChain_FastPathReturnsInputSlice(t *testing.T) {
	leaf := mkCert(t, "leaf")
	root := mkCert(t, "root")
	in := []string{leaf, root}
	out := dedupCertChain(in)
	if &in[0] != &out[0] {
		t.Fatalf("fast path must return the input slice when no duplicates exist; got a different backing array")
	}
}

// ---- end-to-end tests through CreateCertificate -----------------------------

// All e2e tests drive the full gRPC handler so we close the gap that helper
// tests don't exercise the actual call site.

// realChain builds three distinct self-signed certs (leaf, intermediate, root)
// for use in e2e tests.
func realChain(t *testing.T) (leaf, inter, root string) {
	t.Helper()
	return mkCert(t, "leaf-spiffe"), mkCert(t, "Intermediate CA"), mkCert(t, "Root CA")
}

// callCreateCertificate invokes Server.CreateCertificate with a minimal
// authenticated context.
func callCreateCertificate(t *testing.T, srv *Server) []string {
	t.Helper()
	p := &peer.Peer{
		Addr:     &net.IPAddr{IP: net.IPv4(127, 0, 0, 1)},
		AuthInfo: credentials.TLSInfo{},
	}
	ctx := peer.NewContext(context.Background(), p)
	resp, err := srv.CreateCertificate(ctx, &pb.IstioCertificateRequest{
		Csr:              "fake-csr",
		ValidityDuration: 60,
	})
	if err != nil {
		t.Fatalf("CreateCertificate failed: %v", err)
	}
	return resp.CertChain
}

// newSrv constructs a Server suitable for handler-level tests. We bypass
// New(...) because that requires a multicluster.ComponentBuilder; the dedup
// behaviour does not depend on the controller-backed fields, and the existing
// table-driven TestCreateCertificate in server_test.go does the same.
func newSrv(ca CertificateAuthority) *Server {
	return &Server{
		ca: ca,
		Authenticators: []security.Authenticator{
			&mockAuthenticator{identities: []string{"spiffe://cluster.local/ns/foo/sa/bar"}},
		},
		monitoring: newMonitoringMetrics(),
	}
}

// The canonical istio/istio#39001 bug exercised end-to-end through the gRPC
// handler. This is the regression test that would have caught the bug.
func TestCreateCertificate_RootIsNotDuplicatedInResponse(t *testing.T) {
	leaf, inter, root := realChain(t)

	// Operator cert-chain.pem (Istio Makefile format): inter || root.
	certChain := inter + root
	srv := newSrv(&mockca.FakeCA{
		SignedCert:    []byte(leaf),
		KeyCertBundle: util.NewKeyCertBundleFromPem(nil, nil, []byte(certChain), []byte(root), nil),
	})

	fps := fpsOfChain(t, callCreateCertificate(t, srv))
	leafFP, interFP, rootFP := fpOf(t, leaf), fpOf(t, inter), fpOf(t, root)

	if len(fps) != 3 {
		t.Fatalf("expected 3 certs (leaf, inter, root); got %d fps=%v", len(fps), fps)
	}
	if fps[0] != leafFP {
		t.Errorf("position 0 must be leaf; got %s", fps[0])
	}
	if fps[1] != interFP {
		t.Errorf("position 1 must be intermediate; got %s", fps[1])
	}
	if fps[2] != rootFP {
		t.Errorf("position 2 (last) must be root; got %s", fps[2])
	}
	rootCount := 0
	for _, f := range fps {
		if f == rootFP {
			rootCount++
		}
	}
	if rootCount != 1 {
		t.Errorf("root appears %d times in response (expected 1)", rootCount)
	}
}

// Rotation: trust bundle is `oldRoot || newRoot`; both must survive and the
// response must end with the newRoot (the tail of rootCertBytes).
func TestCreateCertificate_RotationBothRootsSurvive(t *testing.T) {
	leaf := mkCert(t, "leaf")
	inter := mkCert(t, "Intermediate CA")
	oldRoot := mkCert(t, "Old Root CA")
	newRoot := mkCert(t, "New Root CA")

	certChain := inter + oldRoot
	trustBundle := oldRoot + newRoot

	srv := newSrv(&mockca.FakeCA{
		SignedCert:    []byte(leaf),
		KeyCertBundle: util.NewKeyCertBundleFromPem(nil, nil, []byte(certChain), []byte(trustBundle), nil),
	})

	fps := fpsOfChain(t, callCreateCertificate(t, srv))
	oldFP, newFP := fpOf(t, oldRoot), fpOf(t, newRoot)

	oldCount, newCount := 0, 0
	for _, f := range fps {
		if f == oldFP {
			oldCount++
		}
		if f == newFP {
			newCount++
		}
	}
	if oldCount != 1 {
		t.Errorf("old root appears %d times (expected 1)", oldCount)
	}
	if newCount != 1 {
		t.Errorf("new root appears %d times (expected 1)", newCount)
	}
	if fps[len(fps)-1] != newFP {
		t.Errorf("response must end with newRoot (trust bundle tail); got %s", fps[len(fps)-1])
	}
}

// Bundle swap mid-flight: a sequential second call with a different
// KeyCertBundle must not be contaminated by state from the first call.
func TestCreateCertificate_BundleSwapDoesNotLeakStaleRoot(t *testing.T) {
	leaf1, inter1, root1 := realChain(t)
	leaf2, inter2, root2 := realChain(t)

	ca := &mockca.FakeCA{
		SignedCert:    []byte(leaf1),
		KeyCertBundle: util.NewKeyCertBundleFromPem(nil, nil, []byte(inter1+root1), []byte(root1), nil),
	}
	srv := newSrv(ca)

	// First call: chain references root1.
	fps1 := fpsOfChain(t, callCreateCertificate(t, srv))
	if got := len(fps1); got != 3 {
		t.Fatalf("first call: want 3 certs, got %d", got)
	}

	// Swap the bundle to a completely different identity hierarchy.
	ca.SignedCert = []byte(leaf2)
	ca.KeyCertBundle = util.NewKeyCertBundleFromPem(nil, nil, []byte(inter2+root2), []byte(root2), nil)

	// Second call must contain only the NEW identities; no leak from call 1.
	fps2 := fpsOfChain(t, callCreateCertificate(t, srv))
	if got := len(fps2); got != 3 {
		t.Fatalf("second call: want 3 certs, got %d", got)
	}
	for _, fp := range fps2 {
		if fp == fpOf(t, leaf1) || fp == fpOf(t, inter1) || fp == fpOf(t, root1) {
			t.Errorf("stale cert from call 1 leaked into call 2 response: %s", fp)
		}
	}
}

// Self-signed mode: certChainBytes is empty; response = [leaf, root]
// unchanged by dedup.
func TestCreateCertificate_SelfSignedModeUnchanged(t *testing.T) {
	leaf := mkCert(t, "leaf")
	root := mkCert(t, "Root CA")

	srv := newSrv(&mockca.FakeCA{
		SignedCert:    []byte(leaf),
		KeyCertBundle: util.NewKeyCertBundleFromPem(nil, nil, nil, []byte(root), nil),
	})

	fps := fpsOfChain(t, callCreateCertificate(t, srv))
	if len(fps) != 2 {
		t.Fatalf("self-signed mode: want 2 certs (leaf, root); got %d", len(fps))
	}
	if fps[0] != fpOf(t, leaf) {
		t.Errorf("position 0 must be leaf; got %s", fps[0])
	}
	if fps[1] != fpOf(t, root) {
		t.Errorf("position 1 must be root; got %s", fps[1])
	}
}
