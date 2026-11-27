//go:build !windows

package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

func openSerial(port string, baud int) (io.ReadWriteCloser, error) {
	exec.Command("stty", "-F", port, strconv.Itoa(baud), "raw", "-echo", "-hupcl").Run()
	return os.OpenFile(port, os.O_RDWR, 0)
}

func listSerialPorts() []map[string]any {
	out := []map[string]any{}
	for _, pat := range []string{"/dev/ttyUSB*", "/dev/ttyACM*", "/dev/cu.usb*"} {
		m, _ := filepath.Glob(pat)
		for _, p := range m {
			out = append(out, map[string]any{"device": p, "description": "Serial port", "arduino": true})
		}
	}
	return out
}
