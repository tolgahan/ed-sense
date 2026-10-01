package window

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/binary"
	"encoding/hex"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"
)

func testCert(t *testing.T, name string, serial int64) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: name},
		NotBefore: time.Unix(0, 0), NotAfter: time.Unix(1<<31, 0)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := asn1.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// signedData is a PKCS #7 SignedData blob holding certs, signed (as far
// as the parser cares) by signer.
func signedData(t *testing.T, signer *x509.Certificate, certs ...*x509.Certificate) []byte {
	t.Helper()
	type issuerSerial struct {
		Issuer asn1.RawValue
		Serial *big.Int
	}
	type signerInfo struct {
		Version int
		ID      issuerSerial
		Digest  asn1.RawValue
	}
	si := mustMarshal(t, signerInfo{Version: 1, ID: issuerSerial{Issuer: asn1.RawValue{FullBytes: signer.RawIssuer}, Serial: signer.SerialNumber},
		Digest: asn1.RawValue{Tag: asn1.TagSequence, IsCompound: true}})
	var all []byte
	for _, c := range certs {
		all = append(all, c.Raw...)
	}
	sd := mustMarshal(t, struct {
		Version int
		Digests asn1.RawValue
		Content asn1.RawValue
		Certs   asn1.RawValue
		Signers asn1.RawValue
	}{
		Version: 1,
		Digests: asn1.RawValue{Tag: asn1.TagSet, IsCompound: true},
		Content: asn1.RawValue{FullBytes: mustMarshal(t, struct{ Type asn1.ObjectIdentifier }{asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 2, 1, 4}})},
		Certs:   asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: all},
		Signers: asn1.RawValue{Tag: asn1.TagSet, IsCompound: true, Bytes: si},
	})
	return mustMarshal(t, struct {
		Type    asn1.ObjectIdentifier
		Content asn1.RawValue
	}{oidSignedData, asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: sd}})
}

func thumbOf(c *x509.Certificate) string {
	sum := sha1.Sum(c.Raw)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func TestPKCS7Signer(t *testing.T) {
	root := testCert(t, "Test root", 1)
	leaf := testCert(t, "Test signer", 7)
	stamp := testCert(t, "Test timestamps", 9)
	blob := signedData(t, leaf, root, leaf, stamp)
	got, err := pkcs7Signer(append(blob, 0, 0, 0)) // WIN_CERTIFICATE pads to 8 bytes
	if err != nil {
		t.Fatal(err)
	}
	if got != thumbOf(leaf) {
		t.Errorf("thumbprint %s, want %s", got, thumbOf(leaf))
	}
	if _, err := pkcs7Signer(signedData(t, leaf, root, stamp)); err == nil {
		t.Error("a signer missing from the certificates was found")
	}
	if _, err := pkcs7Signer([]byte{0x30, 0x03, 0x02, 0x01, 0x01}); err == nil {
		t.Error("garbage was read as a signature")
	}
}

// fakePE is the smallest PE32+ header with a certificate table holding
// blob, or none.
func fakePE(blob []byte) []byte {
	const peAt, opt = 0x80, 0x80 + 24
	b := make([]byte, 0x400)
	copy(b, "MZ")
	binary.LittleEndian.PutUint32(b[0x3c:], peAt)
	copy(b[peAt:], "PE\x00\x00")
	binary.LittleEndian.PutUint16(b[peAt+4+16:], 240) // SizeOfOptionalHeader
	binary.LittleEndian.PutUint16(b[opt:], 0x20b)
	binary.LittleEndian.PutUint32(b[opt+108:], 16) // NumberOfRvaAndSizes
	if blob == nil {
		return b
	}
	at := len(b)
	cert := make([]byte, 8, 8+len(blob)+8)
	cert = append(cert, blob...)
	for len(cert)%8 != 0 {
		cert = append(cert, 0)
	}
	binary.LittleEndian.PutUint32(cert, uint32(8+len(blob)))
	binary.LittleEndian.PutUint16(cert[4:], 0x0200)
	binary.LittleEndian.PutUint16(cert[6:], 2)
	binary.LittleEndian.PutUint32(b[opt+112+4*8:], uint32(at))
	binary.LittleEndian.PutUint32(b[opt+112+4*8+4:], uint32(len(cert)))
	return append(b, cert...)
}

func TestPESignature(t *testing.T) {
	leaf := testCert(t, "Test signer", 7)
	blob := signedData(t, leaf, leaf)
	got, err := peSignature(bytes.NewReader(fakePE(blob)))
	if err != nil || !bytes.Equal(got, blob) {
		t.Fatalf("signature %d bytes, %v", len(got), err)
	}
	if got, err := peSignature(bytes.NewReader(fakePE(nil))); err != nil || got != nil {
		t.Errorf("unsigned: %d bytes, %v", len(got), err)
	}
	if _, err := peSignature(bytes.NewReader([]byte("not an exe at all, just text"))); err == nil {
		t.Error("text was read as an exe")
	}

	dir := t.TempDir()
	path := dir + "/signed.exe"
	if err := os.WriteFile(path, fakePE(blob), 0o644); err != nil {
		t.Fatal(err)
	}
	if thumb, err := signerThumbprint(path); err != nil || thumb != thumbOf(leaf) {
		t.Errorf("signerThumbprint = %s, %v", thumb, err)
	}
}
