package window

import (
	"bytes"
	"crypto/sha1"
	"crypto/x509"
	"encoding/asn1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"math/big"
	"os"
	"strings"
)

// signerThumbprint is the SHA-1 thumbprint of the certificate that signed
// the exe at path (its Authenticode signature), or "" when it has none.
// Only the file is read; nothing is checked online.
func signerThumbprint(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	blob, err := peSignature(f)
	if err != nil || blob == nil {
		return "", err
	}
	return pkcs7Signer(blob)
}

// maxSignature is far more than a signature with its chain and timestamp.
const maxSignature = 1 << 20

var errNotPE = errors.New("not an exe")

// peSignature is the PKCS #7 blob in a PE file's certificate table, or nil.
func peSignature(r io.ReaderAt) ([]byte, error) {
	var dos [64]byte
	if _, err := r.ReadAt(dos[:], 0); err != nil || dos[0] != 'M' || dos[1] != 'Z' {
		return nil, errNotPE
	}
	pe := int64(binary.LittleEndian.Uint32(dos[0x3c:]))
	var hdr [24 + 2]byte // signature, COFF header, optional header magic
	if _, err := r.ReadAt(hdr[:], pe); err != nil || !bytes.Equal(hdr[:4], []byte("PE\x00\x00")) {
		return nil, errNotPE
	}
	opt := pe + 24
	var count, dirs int64 // NumberOfRvaAndSizes, then the data directories
	switch binary.LittleEndian.Uint16(hdr[24:]) {
	case 0x20b: // PE32+
		count, dirs = opt+108, opt+112
	case 0x10b: // PE32
		count, dirs = opt+92, opt+96
	default:
		return nil, errNotPE
	}
	var b [8]byte
	if _, err := r.ReadAt(b[:4], count); err != nil {
		return nil, errNotPE
	}
	const security = 4 // IMAGE_DIRECTORY_ENTRY_SECURITY
	if binary.LittleEndian.Uint32(b[:4]) <= security {
		return nil, nil
	}
	if _, err := r.ReadAt(b[:], dirs+security*8); err != nil {
		return nil, errNotPE
	}
	at, size := int64(binary.LittleEndian.Uint32(b[:4])), int64(binary.LittleEndian.Uint32(b[4:]))
	if at == 0 || size == 0 {
		return nil, nil
	}
	if size < 8 || size > maxSignature {
		return nil, errors.New("odd certificate table")
	}
	table := make([]byte, size)
	if _, err := r.ReadAt(table, at); err != nil {
		return nil, err
	}
	// the first WIN_CERTIFICATE: length, revision, type, then the blob
	length := int64(binary.LittleEndian.Uint32(table))
	const pkcsSignedData = 2
	if length < 8 || length > size || binary.LittleEndian.Uint16(table[6:]) != pkcsSignedData {
		return nil, errors.New("no PKCS #7 signature")
	}
	return table[8:length], nil
}

var oidSignedData = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}

// pkcs7Signer is the thumbprint of the certificate that made the first
// signature in a SignedData blob.
func pkcs7Signer(der []byte) (string, error) {
	var ci struct {
		Type    asn1.ObjectIdentifier
		Content asn1.RawValue `asn1:"explicit,tag:0"`
	}
	if _, err := asn1.Unmarshal(der, &ci); err != nil {
		return "", err
	}
	if !ci.Type.Equal(oidSignedData) {
		return "", errors.New("not signed data")
	}
	var sd struct {
		Version      int
		Digests      asn1.RawValue
		Content      asn1.RawValue
		Certificates asn1.RawValue `asn1:"optional,tag:0"`
		CRLs         asn1.RawValue `asn1:"optional,tag:1"`
		Signers      asn1.RawValue
	}
	// the RawValue keeps the explicit [0]; the SignedData is inside it
	if _, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil {
		return "", err
	}
	var si struct {
		Version int
		ID      struct {
			Issuer asn1.RawValue
			Serial *big.Int
		}
	}
	if _, err := asn1.Unmarshal(sd.Signers.Bytes, &si); err != nil {
		return "", errors.New("the signer is not named by issuer and serial number")
	}
	for rest := sd.Certificates.Bytes; len(rest) > 0; {
		var raw asn1.RawValue
		var err error
		if rest, err = asn1.Unmarshal(rest, &raw); err != nil {
			return "", err
		}
		cert, err := x509.ParseCertificate(raw.FullBytes)
		if err != nil {
			continue
		}
		if bytes.Equal(cert.RawIssuer, si.ID.Issuer.FullBytes) && si.ID.Serial != nil && cert.SerialNumber.Cmp(si.ID.Serial) == 0 {
			sum := sha1.Sum(cert.Raw)
			return strings.ToUpper(hex.EncodeToString(sum[:])), nil
		}
	}
	return "", errors.New("the signer's certificate is not in the signature")
}
