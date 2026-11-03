//go:build !wails

package main

import "fmt"

func main() {
	fmt.Println(`SATSSDV-Go core is ready. Build the desktop UI with: wails build -tags "wails malgo"`)
}
