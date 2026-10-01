package window

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
)

// Wails serves the embedded files on this origin on Windows
// (internal/assetserver/assetserver_windows.go).
const (
	Host   = "wails.localhost"
	Origin = "http://" + Host
)

// MetaCSP is the policy repeated in index.html. A meta tag cannot carry
// frame-ancestors, so the header adds it.
//
// connect-src 'self' is what the Wails runtime needs: calls are fetch
// POSTs to /wails/runtime, and events over 8 KiB are fetched from
// /wails/eventpayload/. Trusted types keep text from the game (names of
// commanders, ships, stations) from ever becoming markup or script.
const MetaCSP = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; " +
	"font-src 'none'; connect-src 'self'; media-src 'none'; object-src 'none'; frame-src 'none'; " +
	"worker-src 'none'; manifest-src 'none'; base-uri 'none'; form-action 'none'; " +
	"require-trusted-types-for 'script'; trusted-types 'none'"

// CSP goes on every response.
const CSP = MetaCSP + "; frame-ancestors 'none'"

// PermissionsPolicy turns off the browser features the page never uses.
const PermissionsPolicy = "accelerometer=(), autoplay=(), bluetooth=(), camera=(), display-capture=(), " +
	"fullscreen=(), gamepad=(), geolocation=(), gyroscope=(), hid=(), idle-detection=(), " +
	"local-fonts=(), magnetometer=(), microphone=(), midi=(), payment=(), " +
	"publickey-credentials-get=(), screen-wake-lock=(), serial=(), usb=(), window-management=()"

// Runtime objects the page may call (pkg/application/messageprocessor.go):
// Call and CancelCall only. Clipboard, Dialog, Window, Browser, Events
// and the rest stay closed.
var allowedCalls = map[[2]int]bool{
	{0, 0}:  true, // Call.ByID
	{10, 0}: true, // cancel a running call
}

// maxCallBody is the largest runtime call body the page sends.
const maxCallBody = 1 << 20

// Guard wraps the Wails asset server. It runs before Wails' own handlers,
// so it sees every request the page makes.
func Guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", CSP)
		h.Set("Permissions-Policy", PermissionsPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		// the cache lives in a folder that every version shares
		h.Set("Cache-Control", "no-store")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")

		// Wails forwards any host that starts with wails.localhost.
		if r.Host != Host {
			http.NotFound(w, r)
			return
		}
		path := r.URL.Path
		switch {
		case path == "/wails/runtime":
			if !callAllowed(r) {
				http.Error(w, "refused", http.StatusForbidden)
				return
			}
		case path == "/wails/runtime.js" || strings.HasPrefix(path, "/wails/eventpayload/"):
			if !readOnly(r) {
				http.Error(w, "refused", http.StatusMethodNotAllowed)
				return
			}
		case strings.HasPrefix(path, "/wails/"):
			// custom.js, transport.js, stream: not used
			http.NotFound(w, r)
			return
		default:
			if !readOnly(r) {
				http.Error(w, "refused", http.StatusMethodNotAllowed)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func readOnly(r *http.Request) bool {
	return r.Method == http.MethodGet || r.Method == http.MethodHead
}

// callAllowed accepts only same-origin JSON posts for the allowed objects.
// A JSON content type makes any cross-origin fetch send a preflight first,
// and the preflight is refused here, so no other page can reach Go.
func callAllowed(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return false
	}
	if o := r.Header.Get("Origin"); o != "" && o != Origin {
		return false
	}
	if s := r.Header.Get("Sec-Fetch-Site"); s != "" && s != "same-origin" {
		return false
	}
	if r.Header.Get("x-wails-chunk-id") != "" {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxCallBody+1))
	if err != nil || len(body) > maxCallBody {
		return false
	}
	var call struct {
		Object *int `json:"object"`
		Method *int `json:"method"`
	}
	if json.Unmarshal(body, &call) != nil || call.Object == nil || call.Method == nil {
		return false
	}
	if !allowedCalls[[2]int{*call.Object, *call.Method}] {
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	return true
}
