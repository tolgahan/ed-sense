//go:build production

package window

// production is true when built with -tags production, which turns off
// the WebView2 DevTools and Wails' dev server.
const production = true
