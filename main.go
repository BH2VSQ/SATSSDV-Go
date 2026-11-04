package main

// SATSSDV-Go is the Go service process launched by the native Qt 6 Widgets UI.
// It exposes a token-authenticated loopback RPC endpoint and event feed; no
// browser engine, WebView or embedded HTML renderer is part of this process.
func main() {
	runBackend()
}
