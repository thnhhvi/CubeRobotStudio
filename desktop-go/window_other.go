//go:build !windows || nowebview

package main

// Native window is Windows-only (WebView2); other platforms use the browser.
func runWindow(url string) bool { return false }
