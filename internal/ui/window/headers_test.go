package window

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/tolgahan/ed-sense/internal/ui/web"
)

func serve(method, host, path, ctype, origin, site, body string) *httptest.ResponseRecorder {
	h := Guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	r := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	if ctype != "" {
		r.Header.Set("Content-Type", ctype)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if site != "" {
		r.Header.Set("Sec-Fetch-Site", site)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestGuard(t *testing.T) {
	const call = `{"object":0,"method":0,"args":{"call-id":"x","methodID":1,"args":[]}}`
	const json = "application/json"
	cases := []struct {
		name                                    string
		method, host, path, ctype, origin, site string
		body                                    string
		want                                    int
	}{
		{"page", "GET", Host, "/", "", "", "", "", http.StatusTeapot},
		{"a module", "GET", Host, "/js/main.js", "", "", "", "", http.StatusTeapot},
		{"runtime.js", "GET", Host, "/wails/runtime.js", "", "", "", "", http.StatusTeapot},
		{"a big event", "GET", Host, "/wails/eventpayload/abc", "", "", "", "", http.StatusTeapot},
		{"other host", "GET", Host + ".example", "/", "", "", "", "", http.StatusNotFound},
		{"custom.js", "HEAD", Host, "/wails/custom.js", "", "", "", "", http.StatusNotFound},
		{"stream", "GET", Host, "/wails/stream/x", "", "", "", "", http.StatusNotFound},
		{"post to a file", "POST", Host, "/index.html", "", "", "", "", http.StatusMethodNotAllowed},
		{"put", "PUT", Host, "/", "", "", "", "", http.StatusMethodNotAllowed},
		{"call", "POST", Host, "/wails/runtime", json, Origin, "same-origin", call, http.StatusTeapot},
		{"call, no origin", "POST", Host, "/wails/runtime", json, "", "", call, http.StatusTeapot},
		{"call, text/plain", "POST", Host, "/wails/runtime", "text/plain", Origin, "", call, http.StatusForbidden},
		{"call, other origin", "POST", Host, "/wails/runtime", json, "null", "", call, http.StatusForbidden},
		{"call, cross-site", "POST", Host, "/wails/runtime", json, "", "cross-site", call, http.StatusForbidden},
		{"call, GET", "GET", Host, "/wails/runtime", json, Origin, "", call, http.StatusForbidden},
		{"preflight", "OPTIONS", Host, "/wails/runtime", "", "http://example.com", "", "", http.StatusForbidden},
		{"cancel", "POST", Host, "/wails/runtime", json, Origin, "", `{"object":10,"method":0}`, http.StatusTeapot},
		{"browser.OpenURL", "POST", Host, "/wails/runtime", json, Origin, "", `{"object":9,"method":0}`, http.StatusForbidden},
		{"events.Emit", "POST", Host, "/wails/runtime", json, Origin, "", `{"object":3,"method":0}`, http.StatusForbidden},
		{"clipboard", "POST", Host, "/wails/runtime", json, Origin, "", `{"object":4,"method":1}`, http.StatusForbidden},
		{"no object", "POST", Host, "/wails/runtime", json, Origin, "", `{"method":0}`, http.StatusForbidden},
		{"too big", "POST", Host, "/wails/runtime", json, Origin, "", `{"object":0,"method":0,"x":"` + strings.Repeat("a", maxCallBody) + `"}`, http.StatusForbidden},
	}
	for _, c := range cases {
		w := serve(c.method, c.host, c.path, c.ctype, c.origin, c.site, c.body)
		if w.Code != c.want {
			t.Errorf("%s: got %d, want %d", c.name, w.Code, c.want)
		}
		hd := w.Header()
		if hd.Get("Content-Security-Policy") != CSP || hd.Get("X-Content-Type-Options") != "nosniff" ||
			hd.Get("Cache-Control") != "no-store" || hd.Get("Permissions-Policy") != PermissionsPolicy {
			t.Errorf("%s: headers %v", c.name, hd)
		}
	}
}

func TestChunkedCallRefused(t *testing.T) {
	h := Guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) }))
	r := httptest.NewRequest("POST", "http://"+Host+"/wails/runtime", strings.NewReader(`{"object":0,"method":0}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("x-wails-chunk-id", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("a chunked call got %d", w.Code)
	}
}

func TestMetaCSPMatches(t *testing.T) {
	page, err := fs.ReadFile(web.FS(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), `content="`+MetaCSP+`"`) {
		t.Error("index.html's meta CSP differs from MetaCSP")
	}
}

// TestWebFiles: the page needs no inline script, style or handler (the
// CSP would block them), links nothing outside, and is plain ASCII.
func TestWebFiles(t *testing.T) {
	inline := regexp.MustCompile(`(?i)<script(?:\s[^>]*)?>[^<]|\sstyle=|\son[a-z]+=|<a\s|href="http|src="http|innerHTML|outerHTML|insertAdjacentHTML|document\.write|eval\(|new Function`)
	n := 0
	err := fs.WalkDir(web.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		n++
		b, err := fs.ReadFile(web.FS(), path)
		if err != nil {
			return err
		}
		for i, c := range b {
			if c > 0x7e || c < 0x20 && c != '\n' && c != '\t' {
				t.Errorf("%s: byte %#x at %d", path, c, i)
				break
			}
		}
		if m := inline.Find(b); m != nil && !strings.HasSuffix(path, ".go") {
			t.Errorf("%s: %q", path, m)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 5 {
		t.Errorf("only %d web files", n)
	}
}
