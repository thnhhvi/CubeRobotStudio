//go:build windows

package main

import (
	webview2 "github.com/jchv/go-webview2"
)

// runWindow shows the UI in a native window (Edge WebView2, built into Windows 10/11).
// Returns false if WebView2 is not available so the caller can fall back to the browser.
func runWindow(url string) bool {
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "Cube Robot Studio",
			Width:  1440,
			Height: 900,
			IconId: 1,
			Center: true,
		},
	})
	if w == nil {
		return false
	}
	defer w.Destroy()
	w.SetSize(1440, 900, webview2.HintNone)
	w.Navigate(url)
	w.Run()
	return true
}
