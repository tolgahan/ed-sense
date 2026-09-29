#!/usr/bin/env python3
"""Check that a signed exe is the CI-built unsigned exe plus one Authenticode
signature, and nothing else.

    same_build.py UNSIGNED SIGNED [--unsigned-sha256 HEX] [--signed-sha256 HEX]
                  [--signer-sha1 HEX ...] [--require-timestamp]
    same_build.py --self-test

Signing a PE file may change exactly three things (PE/COFF spec, "The
Attribute Certificate Table", https://learn.microsoft.com/windows/win32/debug/pe-format):

  - the CheckSum field of the optional header (4 bytes at optional header + 64),
  - the Certificate Table data directory entry (index 4, 8 bytes: a file
    offset, not an RVA, and a size),
  - the attribute certificate table, appended at the end of the file after
    zero padding to an 8-byte boundary.

The check fails on any other difference. It also reads the signature far
enough to check that its digest is the digest of this file, which signer
certificate made it (--signer-sha1) and that it has a timestamp. It does not
check the RSA signature, the chain or revocation; signtool verify on Windows
does that in the sign job.

--unsigned-sha256 and --signed-sha256 must be 64 hex digits when given: an
empty value (a job output that went missing) is bad usage, never a skipped
check. The signed hash is what ties this file to the one signtool verified.

Stdlib only, Python 3.9+. Exit status: 0 OK, 1 check failed, 2 bad usage.
Our own code, written from the specs above. sign.yml pins the SHA-256 of
this file (PIN_SAME_BUILD_PY); change the pin in the same commit.
"""

import argparse
import array
import contextlib
import hashlib
import io
import os
import random
import re
import struct
import sys
from dataclasses import dataclass

PE32 = 0x10B
PE32_PLUS = 0x20B
SECURITY_DIR = 4  # IMAGE_DIRECTORY_ENTRY_SECURITY
WIN_CERT_REVISION_2_0 = 0x0200
WIN_CERT_TYPE_PKCS_SIGNED_DATA = 0x0002

# DER bodies of the object identifiers we look for
OID_SIGNED_DATA = bytes.fromhex("2a864886f70d010702")  # 1.2.840.113549.1.7.2
OID_SPC_INDIRECT_DATA = bytes.fromhex("2b060104018237020104")  # 1.3.6.1.4.1.311.2.1.4
OID_RFC3161_TIMESTAMP = bytes.fromhex("2b060104018237030301")  # 1.3.6.1.4.1.311.3.3.1
OID_COUNTERSIGNATURE = bytes.fromhex("2a864886f70d010906")  # 1.2.840.113549.1.9.6
OID_SHA256 = bytes.fromhex("608648016503040201")
DIGESTS = {
    OID_SHA256: "sha256",
    bytes.fromhex("608648016503040202"): "sha384",
    bytes.fromhex("608648016503040203"): "sha512",
}


class CheckError(Exception):
    """The files are not an unsigned build and its signed copy."""


@dataclass(frozen=True)
class PEHeaders:
    magic: int
    checksum_offset: int
    security_entry_offset: int
    security_offset: int
    security_size: int

    @property
    def masked(self):
        """(start, end) byte ranges that signing may change inside the image."""
        return (
            (self.checksum_offset, self.checksum_offset + 4),
            (self.security_entry_offset, self.security_entry_offset + 8),
        )


@dataclass(frozen=True)
class Signature:
    digest_name: str
    digest: bytes
    signer_sha1: str
    timestamped: bool


def align8(n):
    return (n + 7) & ~7


def u16(data, off):
    return struct.unpack_from("<H", data, off)[0]


def u32(data, off):
    return struct.unpack_from("<I", data, off)[0]


def parse_pe(data):
    """Find the CheckSum and the Certificate Table entry of a PE file."""
    if len(data) < 64 or data[:2] != b"MZ":
        raise CheckError("not a PE file: no MZ header")
    pe = u32(data, 0x3C)  # e_lfanew
    if pe < 64 or pe + 24 > len(data) or data[pe:pe + 4] != b"PE\0\0":
        raise CheckError("not a PE file: no PE signature at e_lfanew 0x%x" % pe)
    size_of_optional = u16(data, pe + 20)
    opt = pe + 24
    if opt + 2 > len(data):
        raise CheckError("optional header is cut off")
    magic = u16(data, opt)
    if magic == PE32:
        count_off, dirs = opt + 92, opt + 96
    elif magic == PE32_PLUS:
        count_off, dirs = opt + 108, opt + 112
    else:
        raise CheckError("unknown optional header magic 0x%x" % magic)
    entry = dirs + 8 * SECURITY_DIR
    if entry + 8 > opt + size_of_optional or entry + 8 > len(data):
        raise CheckError("optional header too short for a Certificate Table entry")
    if u32(data, count_off) <= SECURITY_DIR:
        raise CheckError("NumberOfRvaAndSizes leaves out the Certificate Table")
    return PEHeaders(
        magic=magic,
        checksum_offset=opt + 64,
        security_entry_offset=entry,
        security_offset=u32(data, entry),
        security_size=u32(data, entry + 4),
    )


def pe_checksum(data, checksum_offset):
    """The PE CheckSum as ImageHlp's CheckSumMappedFile computes it: the 16-bit
    one's complement sum of the file's little-endian words, with the CheckSum
    field read as zero, plus the file length."""
    buf = bytearray(data)
    buf[checksum_offset:checksum_offset + 4] = b"\0\0\0\0"
    if len(buf) % 2:
        buf.append(0)
    words = array.array("H", bytes(buf))
    if sys.byteorder == "big":
        words.byteswap()
    total = sum(words)
    while total > 0xFFFF:
        total = (total & 0xFFFF) + (total >> 16)
    return (total + len(data)) & 0xFFFFFFFF


def authenticode_digest(data, headers, name):
    """Authenticode hash of a file whose image is contiguous: every byte before
    the certificate table except the CheckSum and the Certificate Table entry."""
    (c0, c1), (s0, s1) = headers.masked
    end = headers.security_offset if headers.security_size else len(data)
    h = hashlib.new(name)
    h.update(data[:c0])
    h.update(data[c1:s0])
    h.update(data[s1:end])
    return h.digest()


# --- just enough DER to find the digest and the signer in a PKCS#7 blob ---

def der(buf, pos, end):
    """The element at pos: (tag, content start, content end)."""
    if pos + 2 > end:
        raise CheckError("signature: DER element cut off")
    tag, first = buf[pos], buf[pos + 1]
    pos += 2
    if tag & 0x1F == 0x1F:
        raise CheckError("signature: DER tag form not expected")
    if first < 0x80:
        length = first
    else:
        n = first & 0x7F
        if n == 0 or n > 4 or pos + n > end:
            raise CheckError("signature: DER length form not expected")
        length = int.from_bytes(buf[pos:pos + n], "big")
        pos += n
    if pos + length > end:
        raise CheckError("signature: DER element runs past its parent")
    return tag, pos, pos + length


def children(buf, start, end):
    """[(tag, element start, content start, content end)] inside start..end."""
    out = []
    pos = start
    while pos < end:
        tag, cs, ce = der(buf, pos, end)
        out.append((tag, pos, cs, ce))
        pos = ce
    return out


def expect(kids, i, tag, what):
    if i >= len(kids) or kids[i][0] != tag:
        raise CheckError("signature: %s missing" % what)
    return kids[i]


def parse_pkcs7(blob):
    """Digest, signer certificate SHA-1 and timestamp presence of an
    Authenticode PKCS#7 SignedData blob (RFC 2315, Authenticode_PE.docx)."""
    tag, cs, ce = der(blob, 0, len(blob))
    if tag != 0x30:
        raise CheckError("signature: not a DER SEQUENCE")
    if any(blob[ce:]):
        raise CheckError("signature: data after the PKCS#7 blob")
    info = children(blob, cs, ce)
    _, _, ocs, oce = expect(info, 0, 0x06, "contentType")
    if blob[ocs:oce] != OID_SIGNED_DATA:
        raise CheckError("signature: contentType is not signedData")
    _, _, xcs, xce = expect(info, 1, 0xA0, "content")
    _, _, scs, sce = expect(children(blob, xcs, xce), 0, 0x30, "SignedData")
    sd = children(blob, scs, sce)
    expect(sd, 0, 0x02, "SignedData version")
    expect(sd, 1, 0x31, "digestAlgorithms")

    # contentInfo: SpcIndirectDataContent { data, messageDigest DigestInfo }
    _, _, ccs, cce = expect(sd, 2, 0x30, "contentInfo")
    ci = children(blob, ccs, cce)
    _, _, ocs, oce = expect(ci, 0, 0x06, "contentInfo type")
    if blob[ocs:oce] != OID_SPC_INDIRECT_DATA:
        raise CheckError("signature: content is not SpcIndirectDataContent")
    _, _, xcs, xce = expect(ci, 1, 0xA0, "SpcIndirectDataContent")
    _, _, ics, ice = expect(children(blob, xcs, xce), 0, 0x30, "SpcIndirectDataContent")
    _, _, dcs, dce = expect(children(blob, ics, ice), 1, 0x30, "messageDigest")
    di = children(blob, dcs, dce)
    _, _, acs, ace = expect(di, 0, 0x30, "digestAlgorithm")
    _, _, ocs, oce = expect(children(blob, acs, ace), 0, 0x06, "digestAlgorithm OID")
    name = DIGESTS.get(bytes(blob[ocs:oce]))
    if name is None:
        raise CheckError("signature: digest algorithm is not SHA-256/384/512")
    _, _, hcs, hce = expect(di, 1, 0x04, "digest")
    digest = bytes(blob[hcs:hce])

    # certificates [0] IMPLICIT, then signerInfos SET as the last element
    certs = []
    i = 3
    if i < len(sd) and sd[i][0] == 0xA0:
        for t, es, _, ee in children(blob, sd[i][2], sd[i][3]):
            if t == 0x30:
                certs.append((es, ee))
        i += 1
    if i < len(sd) and sd[i][0] == 0xA1:  # crls
        i += 1
    _, _, ics, ice = expect(sd, i, 0x31, "signerInfos")
    signers = children(blob, ics, ice)
    if len(signers) != 1:
        raise CheckError("signature: %d signerInfos, expected 1" % len(signers))
    si = children(blob, signers[0][2], signers[0][3])
    expect(si, 1, 0x30, "issuerAndSerialNumber")
    sid = children(blob, si[1][2], si[1][3])
    if len(sid) != 2:
        raise CheckError("signature: issuerAndSerialNumber is malformed")
    want = (bytes(blob[sid[0][1]:sid[0][3]]), bytes(blob[sid[1][1]:sid[1][3]]))

    signer = None
    for es, ee in certs:
        _, tcs, tce = der(blob, es, ee)  # Certificate
        _, bcs, bce = der(blob, tcs, tce)  # tbsCertificate
        tbs = children(blob, bcs, bce)
        j = 1 if tbs and tbs[0][0] == 0xA0 else 0  # optional [0] version
        if len(tbs) < j + 3:
            continue
        serial = bytes(blob[tbs[j][1]:tbs[j][3]])
        issuer = bytes(blob[tbs[j + 2][1]:tbs[j + 2][3]])
        if (issuer, serial) == want:
            signer = hashlib.sha1(blob[es:ee]).hexdigest().upper()
            break
    if signer is None:
        raise CheckError("signature: the signer's certificate is not in the blob")

    timestamped = False
    if si[-1][0] == 0xA1:  # unsignedAttrs
        for _, _, acs, ace in children(blob, si[-1][2], si[-1][3]):
            attr = children(blob, acs, ace)
            if attr and attr[0][0] == 0x06:
                oid = bytes(blob[attr[0][2]:attr[0][3]])
                timestamped |= oid in (OID_RFC3161_TIMESTAMP, OID_COUNTERSIGNATURE)
    return Signature(name, digest, signer, timestamped)


def parse_certificate_table(data, headers):
    """The PKCS#7 blob of the one WIN_CERTIFICATE in the certificate table."""
    off, size = headers.security_offset, headers.security_size
    if off == 0 or size == 0:
        raise CheckError("signed file has no Certificate Table entry")
    if off % 8:
        raise CheckError("certificate table at 0x%x is not 8-byte aligned" % off)
    if off + size > len(data):
        raise CheckError("certificate table runs past the end of the file")
    tail = data[off + size:]
    if len(tail) >= 8 or any(tail):
        raise CheckError("%d bytes after the certificate table" % len(tail))
    if size < 8:
        raise CheckError("certificate table shorter than a WIN_CERTIFICATE header")
    length, revision, cert_type = struct.unpack_from("<IHH", data, off)
    if revision != WIN_CERT_REVISION_2_0:
        raise CheckError("WIN_CERTIFICATE wRevision 0x%x, expected 0x0200" % revision)
    if cert_type != WIN_CERT_TYPE_PKCS_SIGNED_DATA:
        raise CheckError("WIN_CERTIFICATE wCertificateType 0x%x, expected 0x0002" % cert_type)
    if length < 9 or length > size or align8(length) != align8(size):
        raise CheckError("WIN_CERTIFICATE dwLength %d does not fill the %d byte table "
                         "(only one certificate entry is expected)" % (length, size))
    if any(data[off + length:off + size]):
        raise CheckError("certificate table padding is not zero")
    return data[off + 8:off + length]


def first_difference(a, b, base=0):
    for i, (x, y) in enumerate(zip(a, b)):
        if x != y:
            return base + i
    return base + min(len(a), len(b))


def normalize_sha1(value):
    return value.replace(" ", "").replace(":", "").upper()


def verify(unsigned, signed, signer_sha1=(), require_timestamp=False):
    """Raise CheckError unless signed == unsigned + one Authenticode signature.
    signer_sha1: a thumbprint or a list of them (two during a renewal).
    Returns (headers of the signed file, Signature, stored CheckSum)."""
    u = parse_pe(unsigned)
    if u.security_offset or u.security_size:
        raise CheckError("unsigned file already has a Certificate Table entry")
    s = parse_pe(signed)
    if (s.magic, s.checksum_offset, s.security_entry_offset) != (
            u.magic, u.checksum_offset, u.security_entry_offset):
        raise CheckError("PE headers of the two files do not line up")

    n = len(unsigned)
    if s.security_offset < align8(n):
        raise CheckError("certificate table at 0x%x starts inside the unsigned file "
                         "(0x%x bytes)" % (s.security_offset, n))
    if len(signed) < s.security_offset:
        raise CheckError("signed file is shorter than its certificate table offset")
    if any(signed[n:s.security_offset]):
        raise CheckError("padding before the certificate table is not zero")

    # every byte of the unsigned file, except the two masked fields
    pos = 0
    for start, end in u.masked + ((n, n),):
        if signed[pos:start] != unsigned[pos:start]:
            at = first_difference(signed[pos:start], unsigned[pos:start], pos)
            raise CheckError("signed file differs from the unsigned build at 0x%x" % at)
        pos = end

    blob = parse_certificate_table(signed, s)
    sig = parse_pkcs7(blob)
    if authenticode_digest(signed, s, sig.digest_name) != sig.digest:
        raise CheckError("the signature's %s digest is not the digest of this file"
                         % sig.digest_name)
    pins = [signer_sha1] if isinstance(signer_sha1, str) else list(signer_sha1 or ())
    pins = [normalize_sha1(p) for p in pins]
    if pins and sig.signer_sha1 not in pins:
        raise CheckError("signed by certificate %s, expected %s"
                         % (sig.signer_sha1, " or ".join(pins)))
    if require_timestamp and not sig.timestamped:
        raise CheckError("signature has no timestamp")

    stored = u32(signed, s.checksum_offset)
    computed = pe_checksum(signed, s.checksum_offset)
    if stored != computed:
        raise CheckError("PE CheckSum is 0x%08x, the file sums to 0x%08x" % (stored, computed))
    return s, sig, stored


def strip_signature(signed):
    """The signed file with its signature taken off: cut at the certificate
    table, CheckSum and Certificate Table entry set to 0. Keeps any padding."""
    s = parse_pe(signed)
    out = bytearray(signed[:s.security_offset] if s.security_size else signed)
    for start, end in s.masked:
        out[start:end] = bytes(end - start)
    return bytes(out)


# --- self-test: a fake PE32+ and a fake signature, built in memory ---

def _tlv(tag, *parts):
    body = b"".join(parts)
    n = len(body)
    if n < 0x80:
        head = bytes([tag, n])
    else:
        size = n.to_bytes((n.bit_length() + 7) // 8, "big")
        head = bytes([tag, 0x80 | len(size)]) + size
    return head + body


def _name(cn):
    # Name ::= SEQUENCE { SET { SEQUENCE { OID 2.5.4.3, UTF8String } } }
    return _tlv(0x30, _tlv(0x31, _tlv(0x30, _tlv(0x06, b"\x55\x04\x03"), _tlv(0x0C, cn))))


def _fake_cert(serial, issuer, subject):
    tbs = _tlv(0x30,
               _tlv(0xA0, _tlv(0x02, b"\x02")),
               _tlv(0x02, serial),
               _tlv(0x30, _tlv(0x06, b"\x2a\x86\x48\x86\xf7\x0d\x01\x01\x0b")),
               _name(issuer),
               _tlv(0x30),
               _name(subject))
    return _tlv(0x30, tbs, _tlv(0x30, _tlv(0x06, b"\x2a\x86\x48\x86\xf7\x0d\x01\x01\x0b")),
                _tlv(0x03, b"\x00fake"))


def _fake_pkcs7(digest, certs, signer, timestamp=True, signer_infos=1):
    serial, issuer = signer
    unsigned_attrs = b""
    if timestamp:
        unsigned_attrs = _tlv(0xA1, _tlv(0x30, _tlv(0x06, OID_RFC3161_TIMESTAMP), _tlv(0x31)))
    info = _tlv(0x30,
                _tlv(0x02, b"\x01"),
                _tlv(0x30, _name(issuer), _tlv(0x02, serial)),
                _tlv(0x30, _tlv(0x06, OID_SHA256)),
                _tlv(0x30, _tlv(0x06, b"\x2a\x86\x48\x86\xf7\x0d\x01\x01\x01")),
                _tlv(0x04, b"fake signature"),
                unsigned_attrs)
    indirect = _tlv(0x30,
                    _tlv(0x30, _tlv(0x06, b"\x2b\x06\x01\x04\x01\x82\x37\x02\x01\x0f")),
                    _tlv(0x30, _tlv(0x30, _tlv(0x06, OID_SHA256), _tlv(0x05)),
                         _tlv(0x04, digest)))
    signed_data = _tlv(0x30,
                       _tlv(0x02, b"\x01"),
                       _tlv(0x31, _tlv(0x30, _tlv(0x06, OID_SHA256))),
                       _tlv(0x30, _tlv(0x06, OID_SPC_INDIRECT_DATA), _tlv(0xA0, indirect)),
                       _tlv(0xA0, *certs),
                       _tlv(0x31, *([info] * signer_infos)))
    return _tlv(0x30, _tlv(0x06, OID_SIGNED_DATA), _tlv(0xA0, signed_data))


def _fake_pe(code_len, seed):
    """Header-only PE32+ (no sections) followed by random 'code'."""
    rnd = random.Random(seed)
    pe_off, opt_size = 0x80, 240
    head = bytearray(pe_off + 24 + opt_size)
    head[0:2] = b"MZ"
    struct.pack_into("<I", head, 0x3C, pe_off)
    head[pe_off:pe_off + 4] = b"PE\0\0"
    struct.pack_into("<H", head, pe_off + 4, 0x8664)  # Machine
    struct.pack_into("<H", head, pe_off + 20, opt_size)  # SizeOfOptionalHeader
    struct.pack_into("<H", head, pe_off + 24, PE32_PLUS)  # Magic
    struct.pack_into("<I", head, pe_off + 24 + 108, 16)  # NumberOfRvaAndSizes
    return bytes(head) + bytes(rnd.getrandbits(8) for _ in range(code_len))


def _sign(unsigned, certs, signer, timestamp=True, signer_infos=1):
    """What signtool does: pad, append a WIN_CERTIFICATE, set the entry, set
    the CheckSum. The digest is computed here, apart from verify()."""
    h = parse_pe(unsigned)
    body = bytearray(unsigned + bytes(align8(len(unsigned)) - len(unsigned)))
    (c0, c1), (s0, s1) = h.masked
    digest = hashlib.sha256(bytes(body[:c0] + body[c1:s0] + body[s1:])).digest()
    blob = _fake_pkcs7(digest, certs, signer, timestamp, signer_infos)
    table = struct.pack("<IHH", 8 + len(blob), WIN_CERT_REVISION_2_0,
                        WIN_CERT_TYPE_PKCS_SIGNED_DATA) + blob
    table += bytes(align8(len(table)) - len(table))
    struct.pack_into("<II", body, h.security_entry_offset, len(body), len(table))
    body += table
    struct.pack_into("<I", body, h.checksum_offset, pe_checksum(body, h.checksum_offset))
    return bytes(body)


def _refresh(signed):
    """Recompute the CheckSum after a test edit, so only the edit is wrong."""
    out = bytearray(signed)
    h = parse_pe(out)
    struct.pack_into("<I", out, h.checksum_offset, pe_checksum(out, h.checksum_offset))
    return bytes(out)


def self_test():
    ca = _fake_cert(b"\x01", b"Fake Root", b"Fake CA")
    leaf = _fake_cert(b"\x1e\x8a", b"Fake CA", b"Fake Developer")
    other = _fake_cert(b"\x1e\x8b", b"Fake CA", b"Someone Else")
    certs = (ca, leaf, other)
    pin = hashlib.sha1(leaf).hexdigest().upper()
    by_leaf = (b"\x1e\x8a", b"Fake CA")

    unsigned = _fake_pe(4096, 1)  # 392 + 4096 bytes: already 8-aligned
    signed = _sign(unsigned, certs, by_leaf)
    odd = _fake_pe(4099, 2)  # needs 5 bytes of padding
    odd_signed = _sign(odd, certs, by_leaf)
    h = parse_pe(signed)

    def edit(data, off, value):
        out = bytearray(data)
        out[off] = value
        return bytes(out)

    changed_code = _refresh(edit(signed, 1000, signed[1000] ^ 0xFF))
    appended = _refresh(signed + b"\0" * 8 + b"MZ")
    bad_checksum = edit(signed, h.checksum_offset, signed[h.checksum_offset] ^ 1)
    grafted = bytearray(_fake_pe(4096, 3))
    grafted += signed[len(unsigned):]
    struct.pack_into("<II", grafted, h.security_entry_offset, h.security_offset, h.security_size)
    grafted = _refresh(bytes(grafted))
    second = bytearray(signed + signed[h.security_offset:])
    struct.pack_into("<I", second, h.security_entry_offset + 4, 2 * h.security_size)
    second = _refresh(bytes(second))

    cases = [
        ("aligned PE32+ passes", unsigned, signed, dict(signer_sha1=pin, require_timestamp=True), None),
        ("padded PE32+ passes", odd, odd_signed, dict(signer_sha1=pin.lower()), None),
        ("changed code byte", unsigned, changed_code, {}, "differs from the unsigned build"),
        ("wrong signer", unsigned, signed, dict(signer_sha1="00" * 20), "signed by certificate"),
        ("signer found by issuer and serial", unsigned,
         _sign(unsigned, certs, (b"\x1e\x8b", b"Fake CA")), dict(signer_sha1=pin), "signed by certificate"),
        ("signer cert missing", unsigned, _sign(unsigned, (ca,), by_leaf), {}, "not in the blob"),
        ("no timestamp", unsigned, _sign(unsigned, certs, by_leaf, timestamp=False),
         dict(require_timestamp=True), "no timestamp"),
        ("data after the table", unsigned, appended, {}, "after the certificate table"),
        ("bad CheckSum", unsigned, bad_checksum, {}, "PE CheckSum"),
        ("grafted signature", _fake_pe(4096, 3), grafted, {}, "digest is not the digest"),
        ("two certificate entries", unsigned, second, {}, "only one certificate entry"),
        ("two signerInfos", unsigned, _sign(unsigned, certs, by_leaf, signer_infos=2), {}, "signerInfos"),
        ("unsigned file already signed", signed, signed, {}, "already has a Certificate Table"),
        ("not a PE file", b"x" * 512, signed, {}, "no MZ header"),
    ]
    failed = 0
    for name, u, s, kwargs, want in cases:
        try:
            verify(u, s, **kwargs)
            got = None
        except CheckError as e:
            got = str(e)
        ok = (got is None) if want is None else (got is not None and want in got)
        failed += not ok
        print("%s %s%s" % ("ok  " if ok else "FAIL", name, "" if ok else ": got %r" % got))
    ok = strip_signature(signed) == unsigned and strip_signature(odd_signed)[:len(odd)] == odd
    failed += not ok
    print("%s strip_signature gives back the unsigned file" % ("ok  " if ok else "FAIL"))

    # a hash option that is given must be a hash; checked before any file is read
    usage = [
        ("empty --signed-sha256", ["--signed-sha256", ""]),
        ("blank --unsigned-sha256", ["--unsigned-sha256", "  "]),
        ("short --signed-sha256", ["--signed-sha256", "ab" * 31]),
    ]
    for name, extra in usage:
        code = None
        with contextlib.redirect_stderr(io.StringIO()):
            try:
                main(["missing-unsigned.exe", "missing-signed.exe"] + extra)
            except SystemExit as e:
                code = e.code
            except Exception as e:  # e.g. the missing files were opened
                code = type(e).__name__
        ok = code == 2
        failed += not ok
        print("%s %s is bad usage%s" % ("ok  " if ok else "FAIL", name, "" if ok else ": got %r" % code))
    print("self-test: %d checks, %d failed" % (len(cases) + 1 + len(usage), failed))
    return 1 if failed else 0


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument("unsigned", nargs="?", help="the exe as CI built it")
    p.add_argument("signed", nargs="?", help="the same exe after signing")
    p.add_argument("--unsigned-sha256", help="expected SHA-256 of the unsigned exe")
    p.add_argument("--signed-sha256", help="expected SHA-256 of the signed exe")
    p.add_argument("--signer-sha1", action="append", default=[],
                   help="expected SHA-1 thumbprint of the signing certificate (may repeat)")
    p.add_argument("--require-timestamp", action="store_true",
                   help="fail if the signature is not timestamped")
    p.add_argument("--self-test", action="store_true", help="check this script on fake files")
    args = p.parse_args(argv)
    if args.self_test:
        return self_test()
    if not args.unsigned or not args.signed:
        p.error("UNSIGNED and SIGNED are required")
    for opt in ("unsigned_sha256", "signed_sha256"):
        value = getattr(args, opt)
        if value is not None and not re.fullmatch(r"[0-9A-Fa-f]{64}", value.strip()):
            p.error("--%s needs 64 hex digits, got %r (empty means a job output went missing)"
                    % (opt.replace("_", "-"), value))

    with open(args.unsigned, "rb") as f:
        unsigned = f.read()
    with open(args.signed, "rb") as f:
        signed = f.read()
    u_sha = hashlib.sha256(unsigned).hexdigest()
    s_sha = hashlib.sha256(signed).hexdigest()
    name = os.path.basename(args.signed)
    try:
        if args.unsigned_sha256 is not None and u_sha != args.unsigned_sha256.strip().lower():
            raise CheckError("unsigned exe has SHA-256 %s, expected %s"
                             % (u_sha, args.unsigned_sha256.strip()))
        if args.signed_sha256 is not None and s_sha != args.signed_sha256.strip().lower():
            raise CheckError("signed exe has SHA-256 %s, expected %s"
                             % (s_sha, args.signed_sha256.strip()))
        s, sig, checksum = verify(unsigned, signed, args.signer_sha1, args.require_timestamp)
    except CheckError as e:
        msg = "%s: %s" % (name, e)
        if os.environ.get("GITHUB_ACTIONS") == "true":
            print("::error title=Signed exe check::" + msg)
        print("FAIL " + msg, file=sys.stderr)
        return 1
    print("OK %s: sha256 %s = unsigned sha256 %s + Authenticode signature "
          "(%d bytes at 0x%x, %s digest matches, signer %s, %s, PE CheckSum 0x%08x)"
          % (name, s_sha, u_sha, s.security_size, s.security_offset, sig.digest_name,
             sig.signer_sha1, "timestamped" if sig.timestamped else "no timestamp", checksum))
    return 0


if __name__ == "__main__":
    sys.exit(main())
