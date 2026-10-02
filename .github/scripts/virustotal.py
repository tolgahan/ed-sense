#!/usr/bin/env python3
"""Scan release files on VirusTotal and write a section for the release notes.

    virustotal.py scan --sums SHA256SUMS.txt [--expect FILE=SHA256 ...]
                       --section SECTION.md [--summary SUMMARY.md]
                       [--timeout SECONDS] FILE...
    virustotal.py notes --section SECTION.md [--current FILE ...] NOTES.md

scan first checks each FILE against SHA256SUMS.txt, where it must be listed
under the same name, and against --expect. An empty --expect value means no
expected hash. Nothing is sent before these checks pass. With the API key in
VT_API_KEY it then looks each file up by SHA-256 and uses VirusTotal's
finished analysis when there is one; else it uploads the file and waits for
the analysis. It writes the notes section to SECTION.md and appends a report
to SUMMARY.md. Without a key it only checks the files, says so and exits 0,
and SECTION.md is not written.

notes puts SECTION.md into NOTES.md in place: it replaces the part from
<!-- virustotal --> to <!-- /virustotal -->, or appends it when there is none.
When there is no SECTION.md (the scan failed or had no key), it removes a
section that links a report for a file none of the --current files is, as
after a rebuild of the release. Every other byte of NOTES.md stays as it was.

The notes get only counts, dates and our own file names and hashes, never
text from VirusTotal. The summary also names the engines that flagged a file.
Other text from VirusTotal, such as error messages, goes to the log only.

A public API key allows 4 requests a minute and 500 a day
(https://docs.virustotal.com/reference/public-vs-premium-api), so each
request starts 16 s after the answer to the last one, and a 429, a server
error, a network error or "not available yet" waits 60 s, 120 s, ... (at
most 300 s) before the same request again. The whole scan stops after
--timeout seconds.

Stdlib only, Python 3.9+. Exit status: 0 OK or no key, 1 check or API
failure, 2 bad usage.
"""

import argparse
import datetime
import hashlib
import http.client
import json
import os
import re
import sys
import time
import traceback
import urllib.error
import urllib.parse
import urllib.request
import uuid
from dataclasses import dataclass
from typing import Optional

# VIRUSTOTAL_TEST_API points the script at a mock server; for tests only
API = os.environ.get("VIRUSTOTAL_TEST_API") or "https://www.virustotal.com/api/v3"
REPORT = "https://www.virustotal.com/gui/file/"
REPORT_LINK = re.compile(re.escape(REPORT).encode() + rb"([0-9a-f]{64})")
BIG = 32 * 1024 * 1024  # bigger uploads go to /files/upload_url
INTERVAL = 16.0  # seconds from one answer to the next request: never 5 in one minute
BACKOFF = 60.0
MAX_BACKOFF = 300.0
REQUEST_TIMEOUT = 300  # seconds for one socket read or write; an upload can be slow
MAX_ANSWER = 16 * 1024 * 1024  # a file report is far smaller
LAST_DATE = 4102444800  # 2100-01-01: a later date is not a real scan date
MAX_COUNT = 10000  # VirusTotal has fewer than 100 engines
START = b"<!-- virustotal -->"
END = b"<!-- /virustotal -->"
FLAGGED = ("malicious", "suspicious")
CLEAN = ("undetected", "harmless")


class Problem(Exception):
    """msg is our own text. detail is text from VirusTotal or the network,
    cleaned to one line; it goes to the log only, never to the summary."""

    def __init__(self, msg, detail=""):
        super().__init__(msg)
        self.msg = msg
        self.detail = detail

    def log(self):
        return f"{self.msg} {self.detail}".strip()


class Failure(Problem):
    """A check or API failure: exit status 1."""


class ApiError(Failure):
    def __init__(self, status, msg, detail):
        super().__init__(msg, detail)
        self.status = status


class Retry(Problem):
    """A request that may work if sent again later."""


class NoRedirect(urllib.request.HTTPRedirectHandler):
    # urllib would send the key on to wherever a redirect points
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


OPENER = urllib.request.build_opener(NoRedirect)


@dataclass
class Result:
    flagged: int
    total: int
    date: str
    engines: list
    found: bool  # VirusTotal had it already, nothing was uploaded


@dataclass
class File:
    path: str
    name: str
    sha256: str
    analysis: str = ""
    result: Optional[Result] = None


def clean(text, limit=200):
    """Third-party text for a log line: printable ASCII on one line, cut short."""
    text = text if isinstance(text, str) else ""
    return re.sub(r"[^ -~]+", " ", text)[:limit].strip()


def dig(obj, *keys):
    for key in keys:
        obj = obj.get(key) if isinstance(obj, dict) else None
    return obj


class Client:
    def __init__(self, key, timeout):
        self.key = key
        self.deadline = time.monotonic() + timeout
        self.timeout = timeout
        self.next_at = 0.0

    def request(self, method, url, what, body=None, ctype=None):
        """Sends one request, spaced out and retried, and returns its JSON."""
        backoff = BACKOFF
        last = None
        while True:
            if self.next_at > self.deadline:
                raise Failure(f"no result within {self.timeout:g} s; stopped before: {what}"
                              + (f"; last answer: {last.msg}" if last else ""),
                              last.detail if last else "")
            time.sleep(max(0.0, self.next_at - time.monotonic()))
            try:
                return self.send(method, url, what, body, ctype)
            except Retry as e:
                last = e
                print(f"{what}: {e.log()}, trying again in {backoff:.0f} s")
                self.next_at = time.monotonic() + backoff
                backoff = min(backoff * 2, MAX_BACKOFF)
            finally:
                # the gap counts from the end of each answer: VirusTotal may
                # count an upload only once all of it has arrived
                self.next_at = max(self.next_at, time.monotonic() + INTERVAL)

    def send(self, method, url, what, body, ctype):
        headers = {"x-apikey": self.key, "accept": "application/json"}
        if ctype:
            headers["content-type"] = ctype
        req = urllib.request.Request(url, data=body, method=method, headers=headers)
        # no socket read or write waits past the deadline (0 would mean
        # non-blocking)
        wait = max(1.0, min(REQUEST_TIMEOUT, self.deadline - time.monotonic()))
        try:
            with OPENER.open(req, timeout=wait) as r:
                data = self.read(r, MAX_ANSWER, what)
        except urllib.error.HTTPError as e:
            code, message = self.api_error(e, what)
            # https://docs.virustotal.com/reference/errors: NotAvailableYet
            # is a 400 that works later
            if e.code == 429 or e.code >= 500 or (e.code == 400 and code == "NotAvailableYet"):
                raise Retry(f"HTTP {e.code}", code) from None
            raise ApiError(e.code, f"{what}: HTTP {e.code}", f"{code} {message}".strip()) from None
        except (OSError, http.client.HTTPException) as e:
            raise Retry("network error", f"({clean(str(e))})") from None
        try:
            return json.loads(data)
        except (ValueError, RecursionError):
            raise Failure(f"{what}: VirusTotal's answer is not JSON") from None

    def read(self, r, limit, what):
        """The body of an answer, at most limit bytes, read before the deadline."""
        body = bytearray()
        while True:
            if time.monotonic() > self.deadline:
                raise Failure(f"no result within {self.timeout:g} s; "
                              f"stopped while reading the answer to: {what}")
            # read1 waits for one socket read at most, so the deadline holds
            chunk = r.read1(1 << 16)
            if not chunk:
                break
            body += chunk
            if len(body) > limit:
                raise Failure(f"{what}: VirusTotal's answer is longer than {limit} bytes")
        size = r.headers.get("Content-Length") or ""
        if re.fullmatch(r"[0-9]+", size) and int(size) != len(body):
            raise http.client.IncompleteRead(bytes(body), int(size) - len(body))
        return bytes(body)

    def api_error(self, e, what):
        """The code and message of a VirusTotal error body, cleaned for the log."""
        try:
            body = json.loads(self.read(e, 65536, what))
        except Exception:  # the body is optional: a bad one only costs the detail
            return "", ""
        return clean(dig(body, "error", "code"), 60), clean(dig(body, "error", "message"))


def sha256_of(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def read_sums(path):
    sums = {}
    with open(path, encoding="utf-8", newline="\n") as f:
        for n, line in enumerate(f, 1):
            line = line.rstrip("\n")
            m = re.fullmatch(r"([0-9a-f]{64}) [ *](.+)", line)
            if line and (not m or m[2] in sums):
                raise Failure(f"{path} line {n} is not 'SHA256  NAME', or names a file twice")
            if m:
                sums[m[2]] = m[1]
    return sums


def check_files(paths, sums_path, expect):
    """Hashes the files and checks them; nothing is sent before this passes."""
    sums = read_sums(sums_path)
    sums_name = os.path.basename(sums_path)
    files = []
    for path in paths:
        sha = sha256_of(path)
        if path not in sums:
            raise Failure(f"{path} is not in {sums_name}")
        if sha != sums[path]:
            raise Failure(f"{path} has SHA-256 {sha}, {sums_name} says {sums[path]}")
        want = expect.get(path, "")
        if want and sha != want:
            raise Failure(f"{path} has SHA-256 {sha}, expected {want}")
        print(f"{path}: SHA-256 {sha} matches {sums_name}"
              + (" and the expected hash" if want else "; no expected hash was given"))
        files.append(File(path, os.path.basename(path), sha))
    return files


def result_of(stats, date, results, found, name):
    counts = [dig(stats, k) or 0 for k in FLAGGED + CLEAN]
    if (not all(type(c) is int and 0 <= c <= MAX_COUNT for c in counts)
            or type(date) is not int or not 0 < date < LAST_DATE):
        raise Failure(f"VirusTotal's analysis of {name} has a missing or bad count or date")
    flagged = counts[0] + counts[1]
    engines = sorted(k for k, v in (results if isinstance(results, dict) else {}).items()
                     if dig(v, "category") in FLAGGED)
    day = datetime.datetime.fromtimestamp(date, datetime.timezone.utc).strftime("%Y-%m-%d")
    return Result(flagged, sum(counts), day, engines, found)


def lookup(client, f):
    """VirusTotal's finished analysis of the file, or None."""
    try:
        data = client.request("GET", f"{API}/files/{f.sha256}", f"look up {f.name}")
    except ApiError as e:
        if e.status == 404:
            return None
        raise
    if dig(data, "data", "id") != f.sha256:
        raise Failure(f"look up {f.name}: VirusTotal answered with another file")
    attrs = dig(data, "data", "attributes")
    if not dig(attrs, "last_analysis_date"):
        return None
    r = result_of(dig(attrs, "last_analysis_stats"), dig(attrs, "last_analysis_date"),
                  dig(attrs, "last_analysis_results"), True, f.name)
    return r if r.total else None


def upload_url(client, f):
    data = client.request("GET", f"{API}/files/upload_url", f"get an upload URL for {f.name}")
    url = dig(data, "data")
    got = urllib.parse.urlsplit(url if isinstance(url, str) else "")
    api = urllib.parse.urlsplit(API)
    # the key goes to the API's host only, and over https: the docs show an
    # http:// upload URL
    if got.hostname != api.hostname or not got.path:
        raise Failure(f"the upload URL for {f.name} has another host or no path; "
                      f"the key is only sent to {api.hostname}",
                      f"(host: {clean(got.hostname) or 'none'})")
    return urllib.parse.urlunsplit((api.scheme, api.netloc, got.path, got.query, ""))


def upload(client, f):
    with open(f.path, "rb") as fh:
        content = fh.read()
    if hashlib.sha256(content).hexdigest() != f.sha256:
        raise Failure(f"{f.path} changed after it was checked")
    boundary = uuid.uuid4().hex
    filename = re.sub(r"[^A-Za-z0-9._-]", "_", f.name)
    body = b"".join([
        f"--{boundary}\r\n"
        f'Content-Disposition: form-data; name="file"; filename="{filename}"\r\n'
        "Content-Type: application/octet-stream\r\n\r\n".encode(),
        content,
        f"\r\n--{boundary}--\r\n".encode(),
    ])
    # the 32 MB limit is on what is sent, so the form around the file counts
    url = upload_url(client, f) if len(body) > BIG else f"{API}/files"
    data = client.request("POST", url, f"upload {f.name}", body,
                          f"multipart/form-data; boundary={boundary}")
    analysis = dig(data, "data", "id")
    if not isinstance(analysis, str) or not analysis:
        raise Failure(f"upload {f.name}: VirusTotal's answer has no analysis id")
    print(f"{f.name}: uploaded, analysis {clean(analysis, 120)}")
    return analysis


def poll(client, f):
    """The finished analysis of an uploaded file, or None while it runs."""
    data = client.request("GET", f"{API}/analyses/{urllib.parse.quote(f.analysis, safe='=')}",
                          f"check the analysis of {f.name}")
    attrs = dig(data, "data", "attributes")
    status = dig(attrs, "status")
    if status in ("queued", "in-progress"):
        print(f"{f.name}: {status}")
        return None
    if status != "completed":
        raise Failure(f"the analysis of {f.name} has an unknown status",
                      f"({clean(str(status), 40)!r})")
    sha = dig(data, "meta", "file_info", "sha256")
    if sha is not None and sha != f.sha256:
        raise Failure(f"the analysis of {f.name} is about another file")
    r = result_of(dig(attrs, "stats"), dig(attrs, "date"), dig(attrs, "results"), False, f.name)
    if not r.total:
        raise Failure(f"no engine gave a verdict on {f.name}")
    return r


def scan(client, files):
    for f in files:
        f.result = lookup(client, f)
        if f.result:
            print(f"{f.name}: VirusTotal has an analysis from {f.result.date}, no upload")
    for f in files:
        if not f.result:
            f.analysis = upload(client, f)
    pending = [f for f in files if not f.result]
    while pending:
        for f in list(pending):
            f.result = poll(client, f)
            if f.result:
                pending.remove(f)


def section(files):
    lines = [START.decode(),
             "**VirusTotal.** How many antivirus engines flagged each file, "
             "out of those that gave a verdict:",
             ""]
    for f in files:
        r = f.result
        lines.append(f"- `{f.name}`: {r.flagged} of {r.total}, scanned {r.date} "
                     f"([report]({REPORT}{f.sha256}))")
    lines.append(END.decode())
    return "\n".join(lines) + "\n"


def summary(files):
    lines = ["### VirusTotal", "",
             "| File | SHA-256 | Flagged | Scanned | Report |",
             "|---|---|---|---|---|"]
    for f in files:
        r = f.result
        how = "found, no upload" if r.found else "uploaded now"
        lines.append(f"| `{f.name}` | `{f.sha256}` | {r.flagged} of {r.total} | {r.date} "
                     f"| [{how}]({REPORT}{f.sha256}) |")
    lines.append("")
    for f in files:
        if f.result.engines:
            names = ", ".join(f"`{re.sub(r'[^A-Za-z0-9 ._+()-]', '', e)[:40]}`"
                              for e in f.result.engines)
            lines.append(f"- `{f.name}` flagged by: {names}")
    return "\n".join(lines).rstrip("\n") + "\n\n"


def append(path, text):
    if path:
        with open(path, "a", encoding="utf-8", newline="\n") as f:
            f.write(text)


def annotate(kind, msg):
    if os.environ.get("GITHUB_ACTIONS") == "true":
        print(f"::{kind} title=VirusTotal::" + msg.replace("%", "%25"))


def run_scan(args):
    # a section is there only after a full scan
    if os.path.exists(args.section):
        os.remove(args.section)
    files = check_files(args.files, args.sums, dict(args.expect))
    key = os.environ.get("VT_API_KEY", "").strip()
    if not key:
        msg = ("No VirusTotal API key, so nothing was scanned. To scan releases, add the "
               "secret VT_API_KEY to the virustotal environment.")
        annotate("notice", msg)
        print(msg)
        append(args.summary, f"### VirusTotal\n\nThe files match {os.path.basename(args.sums)}. {msg}\n\n")
        return 0
    if not re.fullmatch(r"[!-~]+", key):
        raise Failure("VT_API_KEY has characters an API key does not have")
    scan(Client(key, args.timeout), files)
    with open(args.section, "w", encoding="ascii", newline="\n") as f:
        f.write(section(files))
    append(args.summary, summary(files))
    for f in files:
        print(f"{f.name}: flagged by {f.result.flagged} of {f.result.total} engines, "
              f"{REPORT}{f.sha256}")
    return 0


def find_section(notes, path):
    """Where the section is in the notes, as (start, end), or None."""
    starts, ends = notes.count(START), notes.count(END)
    if starts == ends == 0:
        return None
    if starts == ends == 1 and notes.index(START) < notes.index(END):
        return notes.index(START), notes.index(END) + len(END)
    raise Failure(f"{path} has {starts} {START.decode()} and {ends} {END.decode()}; "
                  "fix the notes by hand")


def drop(notes, start, end):
    """The notes without the section, the line end after it and the blank
    line that was put before it."""
    head, tail = notes[:start], notes[end:]
    for eol in (b"\r\n", b"\n"):
        if tail.startswith(eol):
            tail = tail[len(eol):]
            break
    for eol in (b"\r\n", b"\n"):
        if head.endswith(eol + eol):
            head = head[:-len(eol)]
            break
    return head + tail


def run_notes(args):
    with open(args.notes, "rb") as f:
        old = f.read()
    where = find_section(old, args.notes)
    if os.path.exists(args.section):
        with open(args.section, "rb") as f:
            new = f.read()
        if not new.startswith(START) or not new.endswith(END + b"\n"):
            raise Failure(f"{args.section} is not a VirusTotal section")
        new = new[:-1]
        if where is None:
            sep = b"" if not old else b"\n" if old.endswith(b"\n") else b"\n\n"
            out, done = old + sep + new + b"\n", "VirusTotal section appended"
        else:
            out, done = old[:where[0]] + new + old[where[1]:], "VirusTotal section replaced"
    elif not args.current:
        raise Failure(f"there is no {args.section} and no --current file to check the notes against")
    elif where is None:
        out, done = old, "no VirusTotal section, nothing to change"
    else:
        linked = set(REPORT_LINK.findall(old[where[0]:where[1]]))
        have = {sha256_of(p).encode() for p in args.current}
        if not linked:
            out, done = old, "the VirusTotal section links no report, so it stays as it is"
        elif linked - have:
            out = drop(old, *where)
            done = ("VirusTotal section removed: it links a report for a file that "
                    "this release no longer has")
            annotate("warning", done)
        else:
            out, done = old, "the VirusTotal section still links this release's files"
    if out != old:
        with open(args.notes, "wb") as f:
            f.write(out)
    print(f"{args.notes}: {done}")
    return 0


def expect_arg(text):
    path, sep, value = text.rpartition("=")
    value = value.strip().lower()
    if not sep or not path or (value and not re.fullmatch(r"[0-9a-f]{64}", value)):
        raise argparse.ArgumentTypeError(f"want FILE=SHA256 with 64 hex digits or nothing, got {text!r}")
    return path, value


def fail(args, e):
    annotate("error", e.log())
    print(f"FAIL {e.log()}", file=sys.stderr)
    if args.command == "scan":
        # the summary renders markdown, so it gets our own text only
        more = " The job log has the details." if e.detail else ""
        append(args.summary, f"### VirusTotal\n\nFailed: {e.msg}.{more}\n\n")
    return 1


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    sub = p.add_subparsers(dest="command", required=True)
    s = sub.add_parser("scan", help="check the files, scan them and write the section")
    s.add_argument("files", nargs="+", metavar="FILE", help="a file, named as in SHA256SUMS.txt")
    s.add_argument("--sums", required=True, help="the SHA256SUMS.txt that lists every FILE")
    s.add_argument("--expect", action="append", default=[], type=expect_arg, metavar="FILE=SHA256",
                   help="the SHA-256 FILE must have; empty means none")
    s.add_argument("--section", required=True, help="where to write the notes section")
    s.add_argument("--summary", help="markdown file to append the report to")
    s.add_argument("--timeout", type=float, default=1200.0, help="seconds for the whole scan")
    n = sub.add_parser("notes", help="put the section into a notes file, or drop a stale one")
    n.add_argument("notes", help="the notes file, changed in place")
    n.add_argument("--section", required=True, help="the section from scan; may be missing")
    n.add_argument("--current", action="append", default=[], metavar="FILE",
                   help="a file of the release as it is now")
    args = p.parse_args(argv)
    if args.command == "scan":
        unknown = sorted(set(dict(args.expect)) - set(args.files))
        if unknown:
            p.error(f"--expect names a file that is not scanned: {', '.join(unknown)}")
        if args.timeout <= 0:
            p.error("--timeout must be more than 0")
    try:
        return run_scan(args) if args.command == "scan" else run_notes(args)
    except Failure as e:
        return fail(args, e)
    except OSError as e:  # our own files
        return fail(args, Failure(str(e)))
    except Exception as e:  # a bug, or an answer nobody expected: still a clear end
        lines = [fr.lineno for fr in traceback.extract_tb(e.__traceback__)
                 if os.path.basename(fr.filename) == os.path.basename(__file__)]
        where = f" at line {lines[-1]} of {os.path.basename(__file__)}" if lines else ""
        return fail(args, Failure(f"unexpected {type(e).__name__}{where}", f"({clean(str(e))})"))


if __name__ == "__main__":
    # progress shows in the job log as it happens
    sys.stdout.reconfigure(line_buffering=True)
    sys.exit(main())
