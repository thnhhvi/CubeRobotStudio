//go:build windows

package main

import (
	"errors"
	"fmt"
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

func openSerial(port string, baud int) (*winSerial, error) {
	path, _ := syscall.UTF16PtrFromString(`\\.\` + port)
	h, err := syscall.CreateFile(path, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil,
		syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %v", port, err)
	}
	var d dcb
	d.DCBlength = uint32(unsafe.Sizeof(d))
	if r, _, e := procGetCommState.Call(uintptr(h), uintptr(unsafe.Pointer(&d))); r == 0 {
		syscall.CloseHandle(h)
		return nil, fmt.Errorf("GetCommState: %v", e)
	}
	d.BaudRate = uint32(baud)
	d.Flags = 1 | (1 << 4) | (1 << 12) // fBinary, DTR enable, RTS enable
	d.ByteSize = 8
	d.Parity = 0
	d.StopBits = 0
	if r, _, e := procSetCommState.Call(uintptr(h), uintptr(unsafe.Pointer(&d))); r == 0 {
		syscall.CloseHandle(h)
		return nil, fmt.Errorf("SetCommState: %v", e)
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

// listSerialPorts probes COM1..COM64 (ports in use by another program show as busy)
func listSerialPorts() []map[string]any {
	out := []map[string]any{}
	for i := 1; i <= 64; i++ {
		name := fmt.Sprintf("COM%d", i)
		path, _ := syscall.UTF16PtrFromString(`\\.\` + name)
		h, err := syscall.CreateFile(path, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil,
			syscall.OPEN_EXISTING, 0, 0)
		if err == nil {
			syscall.CloseHandle(h)
			out = append(out, map[string]any{"device": name, "description": "Serial port", "arduino": i > 2})
		} else if err == syscall.ERROR_ACCESS_DENIED {
			out = append(out, map[string]any{"device": name, "description": "Serial port (in use)", "arduino": false})
		}
	}
	return out
}
