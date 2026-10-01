//go:build !production

package window

// production is false without -tags production. The window then refuses
// to open: a dev build of Wails has DevTools on and follows
// FRONTEND_DEVSERVER_URL.
const production = false
