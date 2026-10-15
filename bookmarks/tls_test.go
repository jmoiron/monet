package bookmarks

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type testCert struct {
	cert *x509.Certificate
	der  []byte
	key  *ecdsa.PrivateKey
}

func makeCert(t *testing.T, tmpl *x509.Certificate, parent *testCert) *testCert {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl.SerialNumber = big.NewInt(time.Now().UnixNano())
	tmpl.NotBefore = time.Now().Add(-time.Hour)
	tmpl.NotAfter = time.Now().Add(time.Hour)
	signer, signKey := tmpl, key
	if parent != nil {
		signer, signKey = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, signer, &key.PublicKey, signKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	return &testCert{cert: cert, der: der, key: key}
}

func serveTLS(t *testing.T, tc *testCert) *httptest.Server {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<title>secure</title>"))
	}))
	// only the leaf is sent: the "forgot the intermediate" misconfiguration
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{tc.der}, PrivateKey: tc.key}}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func TestMissingIntermediateFetchedViaAIA(t *testing.T) {
	root := makeCert(t, &x509.Certificate{Subject: pkix.Name{CommonName: "root"}, IsCA: true,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}, nil)
	inter := makeCert(t, &x509.Certificate{Subject: pkix.Name{CommonName: "inter"}, IsCA: true,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}, root)

	aia := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(inter.der)
	}))
	defer aia.Close()

	leaf := makeCert(t, &x509.Certificate{Subject: pkix.Name{CommonName: "leaf"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IssuingCertificateURL: []string{aia.URL}}, inter)
	srv := serveTLS(t, leaf)

	roots := x509.NewCertPool()
	roots.AddCert(root.cert)

	m, err := FetchMetadata(context.Background(), newBrowserClientRoots(roots), srv.URL)
	if err != nil {
		t.Fatalf("expected chain to be completed via AIA: %v", err)
	}
	if m.Title != "secure" {
		t.Errorf("title = %q", m.Title)
	}

	// without the AIA URL the same server must still be rejected
	leaf2 := makeCert(t, &x509.Certificate{Subject: pkix.Name{CommonName: "leaf2"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}, inter)
	srv2 := serveTLS(t, leaf2)
	if _, err := FetchMetadata(context.Background(), newBrowserClientRoots(roots), srv2.URL); err == nil {
		t.Error("incomplete chain with no AIA url should fail")
	}
}

func TestSelfSignedStillRejected(t *testing.T) {
	self := makeCert(t, &x509.Certificate{Subject: pkix.Name{CommonName: "self"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}, nil)
	srv := serveTLS(t, self)
	if _, err := FetchMetadata(context.Background(), newBrowserClientRoots(nil), srv.URL); err == nil {
		t.Fatal("self-signed certificate must be rejected")
	}
}

func TestWrongHostRejected(t *testing.T) {
	root := makeCert(t, &x509.Certificate{Subject: pkix.Name{CommonName: "root"}, IsCA: true,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}, nil)
	leaf := makeCert(t, &x509.Certificate{Subject: pkix.Name{CommonName: "leaf"},
		DNSNames:    []string{"other.example"},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}, root)
	srv := serveTLS(t, leaf)
	roots := x509.NewCertPool()
	roots.AddCert(root.cert)
	if _, err := FetchMetadata(context.Background(), newBrowserClientRoots(roots), srv.URL); err == nil {
		t.Fatal("certificate for another host must be rejected")
	}
}
