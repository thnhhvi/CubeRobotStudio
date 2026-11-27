// Cube Robot Studio - laptop app (single native executable).
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// AppVersion is shown in the window and reported to the phone.
const AppVersion = "1.6"

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	hideWindow(cmd)
	cmd.Start()
}

func main() {
	noBrowser := flag.Bool("no-browser", false, "server only: do not open the UI window")
	flag.Parse()

	c := NewController()
	mux := newMux(c)
	ips := localIPs()

	go func() {
		if err := http.ListenAndServe(fmt.Sprintf(":%d", httpPort), mux); err != nil {
			log.Fatalf("HTTP server: %v (is Cube Robot Studio already running?)", err)
		}
	}()
	go beacon(c)
	go adbReverseLoop(c)

	url := fmt.Sprintf("http://127.0.0.1:%d/", httpPort)
	fmt.Println("==============================================")
	fmt.Println(" Cube Robot Studio v" + AppVersion)
	fmt.Println("==============================================")
	fmt.Println(" Laptop UI :", url)
	fmt.Println(" Phone app (APK): scan the QR code in the window, or type code", c.PairCode())
	fmt.Println(" Manual address for the phone app:")
	for _, ip := range ips {
		fmt.Printf("     %s:%d\n", ip, httpPort)
	}
	fmt.Println(" Close this window to quit.")
	fmt.Println("==============================================")
	if !*noBrowser {
		time.Sleep(300 * time.Millisecond)
		if runWindow(url) {
			os.Exit(0) // window closed -> quit (also stops the robot link)
		}
		openBrowser(url)
	}
	select {}
}
