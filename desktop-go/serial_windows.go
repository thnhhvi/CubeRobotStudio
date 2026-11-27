//go:build windows

package main

import (
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	procGetCommState    = kernel32.NewProc("GetCommState")
	procSetCommState    = kernel32.NewProc("SetCommState")
	procSetCommTimeouts = kernel32.NewProc("SetCommTimeouts")
	procPurgeComm       = kernel32.NewProc("PurgeComm")
)

type dcb struct {
	DCBlength  uint32
	BaudRate   uint32
	Flags      uint32
	wReserved  uint16
	XonLim     uint16
	XoffLim    uint16
	ByteSize   byte
	Parity     byte
	StopBits   byte
	XonChar    byte
	XoffChar   byte
	ErrorChar  byte
	EofChar    byte
	EvtChar    byte
	wReserved1 uint16
}

type commTimeouts struct {
	ReadIntervalTimeout         uint32
	ReadTotalTimeoutMultiplier  uint32
	ReadTotalTimeoutConstant    uint32
	WriteTotalTimeoutMultiplier uint32
	WriteTotalTimeoutConstant   uint32
}

// The handle is synchronous: Windows serialises I/O on it, so a ReadFile that is
// waiting would block WriteFile. Reads use a short timeout and every call holds io,
// which hands the handle to a waiting writer between reads.
type winSerial struct {
	h      syscall.Handle
	closed int32
	io     sync.Mutex
}

// openSerial opens the port; a USB-serial chip that just reset (servo power dip, cable)
// can refuse the settings for a moment, so it is tried a few times.
func openSerial(port string, baud int) (*winSerial, error) {
	var err error
	for try := 0; try < 4; try++ {
		var s *winSerial
		if s, err = openSerialOnce(port, baud); err == nil {
			return s, nil
		}
		time.Sleep(800 * time.Millisecond)
	}
	return nil, err
}

func openSerialOnce(port string, baud int) (*winSerial, error) {
	path, _ := syscall.UTF16PtrFromString(`\\.\` + port)
	h, err := syscall.CreateFile(path, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil,
		syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		return nil, errors.New(friendlySerialError(port, err))
	}
	var d dcb
	d.DCBlength = uint32(unsafe.Sizeof(d))
	if r, _, e := procGetCommState.Call(uintptr(h), uintptr(unsafe.Pointer(&d))); r == 0 {
		syscall.CloseHandle(h)
		return nil, errors.New(friendlySerialError(port, e))
	}
	d.BaudRate = uint32(baud)
	d.Flags = 1 | (1 << 4) | (1 << 12) // fBinary, DTR enable, RTS enable
	d.ByteSize = 8
	d.Parity = 0
	d.StopBits = 0
	if r, _, e := procSetCommState.Call(uintptr(h), uintptr(unsafe.Pointer(&d))); r == 0 {
		syscall.CloseHandle(h)
		return nil, errors.New(friendlySerialError(port, e))
	}
	t := commTimeouts{0xFFFFFFFF, 0xFFFFFFFF, 20, 0, 1000}
	procSetCommTimeouts.Call(uintptr(h), uintptr(unsafe.Pointer(&t)))
	procPurgeComm.Call(uintptr(h), 0x0008|0x0004) // RXCLEAR | TXCLEAR
	return &winSerial{h: h}, nil
}

func (s *winSerial) Read(p []byte) (int, error) {
	for {
		if atomic.LoadInt32(&s.closed) != 0 {
			return 0, errors.New("closed")
		}
		var n uint32
		s.io.Lock()
		err := syscall.ReadFile(s.h, p, &n, nil)
		s.io.Unlock()
		if err != nil {
			return 0, err
		}
		if n > 0 {
			return int(n), nil
		}
		time.Sleep(3 * time.Millisecond) // let a pending write in
	}
}

func (s *winSerial) Write(p []byte) (int, error) {
	s.io.Lock()
	defer s.io.Unlock()
	var n uint32
	err := syscall.WriteFile(s.h, p, &n, nil)
	return int(n), err
}

func (s *winSerial) Close() error {
	if atomic.CompareAndSwapInt32(&s.closed, 0, 1) {
		return syscall.CloseHandle(s.h)
	}
	return nil
}

// ---------------------------------------------------------------- port listing
// Ports are listed the way Device Manager does (SetupAPI, "Ports (COM & LPT)"), with
// their friendly names - nothing is opened, so the Arduino is not reset and busy ports
// still show. The SERIALCOMM registry key adds any port SetupAPI does not report.

var (
	setupapi                        = syscall.NewLazyDLL("setupapi.dll")
	procSetupDiGetClassDevsW        = setupapi.NewProc("SetupDiGetClassDevsW")
	procSetupDiEnumDeviceInfo       = setupapi.NewProc("SetupDiEnumDeviceInfo")
	procSetupDiGetDeviceRegistryPro = setupapi.NewProc("SetupDiGetDeviceRegistryPropertyW")
	procSetupDiDestroyDeviceInfo    = setupapi.NewProc("SetupDiDestroyDeviceInfoList")
	advapi32                        = syscall.NewLazyDLL("advapi32.dll")
	procRegEnumValueW               = advapi32.NewProc("RegEnumValueW")
)

type spDevinfoData struct {
	cbSize    uint32
	classGUID [16]byte
	devInst   uint32
	reserved  uintptr
}

// GUID_DEVCLASS_PORTS {4D36E978-E325-11CE-BFC1-08002BE10318}
var guidDevclassPorts = [16]byte{0x78, 0xE9, 0x36, 0x4D, 0x25, 0xE3, 0xCE, 0x11,
	0xBF, 0xC1, 0x08, 0x00, 0x2B, 0xE1, 0x03, 0x18}

const (
	digcfPresent     = 0x2
	spdrpHardwareID  = 0x1
	spdrpFriendlyNam = 0xC
)

func devProp(h uintptr, d *spDevinfoData, prop uint32) string {
	buf := make([]uint16, 512)
	var typ, req uint32
	r, _, _ := procSetupDiGetDeviceRegistryPro.Call(h, uintptr(unsafe.Pointer(d)), uintptr(prop),
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2),
		uintptr(unsafe.Pointer(&req)))
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf) // first string of a multi-sz is enough
}

var comRe = regexp.MustCompile(`\((COM\d+)\)`)

func listSerialPorts() []map[string]any {
	out := []map[string]any{}
	seen := map[string]bool{}
	h, _, _ := procSetupDiGetClassDevsW.Call(uintptr(unsafe.Pointer(&guidDevclassPorts)), 0, 0, digcfPresent)
	if h != 0 && h != ^uintptr(0) {
		for i := uint32(0); ; i++ {
			var d spDevinfoData
			d.cbSize = uint32(unsafe.Sizeof(d))
			if r, _, _ := procSetupDiEnumDeviceInfo.Call(h, uintptr(i), uintptr(unsafe.Pointer(&d))); r == 0 {
				break
			}
			name := devProp(h, &d, spdrpFriendlyNam)
			m := comRe.FindStringSubmatch(name)
			if m == nil {
				continue // LPT and other non-COM ports
			}
			hw := strings.ToUpper(devProp(h, &d, spdrpHardwareID))
			seen[m[1]] = true
			out = append(out, map[string]any{"device": m[1], "description": name,
				"arduino": isArduinoHW(hw, name), "bluetooth": strings.HasPrefix(hw, "BTHENUM")})
		}
		procSetupDiDestroyDeviceInfo.Call(h)
	}
	// anything SetupAPI missed
	for dev, port := range serialCommRegistry() {
		if seen[port] {
			continue
		}
		seen[port] = true
		bt := strings.Contains(strings.ToLower(dev), "bth")
		out = append(out, map[string]any{"device": port, "description": port + " (" + dev + ")",
			"arduino": strings.Contains(strings.ToUpper(dev), "USBSER"), "bluetooth": bt})
	}
	sort.Slice(out, func(a, b int) bool { return comNum(out[a]["device"].(string)) < comNum(out[b]["device"].(string)) })
	return out
}

// USB IDs of Arduino boards and the usual USB-serial chips on Nano clones
func isArduinoHW(hw, name string) bool {
	for _, k := range []string{"VID_2341", "VID_2A03", "VID_1A86", "VID_0403", "VID_10C4", "VID_067B"} {
		if strings.Contains(hw, k) {
			return true
		}
	}
	n := strings.ToLower(name)
	return strings.Contains(n, "arduino") || strings.Contains(n, "ch340") || strings.Contains(n, "ch341")
}

func comNum(s string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(strings.ToUpper(s), "COM"))
	return n
}

// HKLM\HARDWARE\DEVICEMAP\SERIALCOMM: value name = device (\Device\USBSER000), data = COMx
func serialCommRegistry() map[string]string {
	out := map[string]string{}
	var key syscall.Handle
	path, _ := syscall.UTF16PtrFromString(`HARDWARE\DEVICEMAP\SERIALCOMM`)
	if syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, path, 0, syscall.KEY_READ, &key) != nil {
		return out
	}
	defer syscall.RegCloseKey(key)
	for i := uint32(0); ; i++ {
		name := make([]uint16, 256)
		data := make([]uint16, 64)
		nl, dl := uint32(len(name)), uint32(len(data)*2)
		var typ uint32
		r, _, _ := procRegEnumValueW.Call(uintptr(key), uintptr(i), uintptr(unsafe.Pointer(&name[0])),
			uintptr(unsafe.Pointer(&nl)), 0, uintptr(unsafe.Pointer(&typ)),
			uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&dl)))
		if r != 0 {
			break
		}
		out[syscall.UTF16ToString(name)] = syscall.UTF16ToString(data)
	}
	return out
}

// friendlySerialError explains Windows error codes from opening / reading a port.
func friendlySerialError(port string, err error) string {
	if errno, ok := err.(syscall.Errno); ok {
		switch errno {
		case 2, 3, 1167: // FILE_NOT_FOUND, PATH_NOT_FOUND, DEVICE_NOT_CONNECTED
			return port + " disappeared from USB. Usually the servos pull the Arduino's 5 V down " +
				"(they must run from the external 5 V supply, not USB), or a loose cable."
		case 5: // ACCESS_DENIED
			return port + " is in use by another program (Arduino IDE Serial Monitor?)."
		case 31, 22: // GEN_FAILURE ("A device attached to the system is not functioning"), BAD_COMMAND
			return port + " is not responding: the Arduino's USB chip has hung, usually after the servos " +
				"pulled its 5 V down. Unplug the Arduino's USB cable, plug it back in, then Connect again " +
				"(power the servos from a separate 5-6 V supply to stop it happening)."
		}
	}
	return port + ": " + err.Error()
}
