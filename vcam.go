package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	clsidStr   = "{6E0F4A6B-8F2D-4C55-9A3E-5B712C84D107}"
	cameraName = "PhoneCam"
	dllName    = "phonecam_vcam.dll"
)

// The Frame Server service (LOCAL SERVICE) must be able to read the DLL, so it
// cannot live in the user's profile; it goes to %ProgramData%\PhoneCam.
func installDir() string   { return filepath.Join(os.Getenv("ProgramData"), "PhoneCam") }
func installedDLL() string { return filepath.Join(installDir(), dllName) }

func registeredPath() string {
	var k syscall.Handle
	path := `SOFTWARE\Classes\CLSID\` + clsidStr + `\InprocServer32`
	if syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, u16(path), 0, syscall.KEY_READ|0x0100 /*WOW64_64KEY*/, &k) != nil {
		return ""
	}
	defer syscall.RegCloseKey(k)
	buf := make([]uint16, 1024)
	n := uint32(len(buf) * 2)
	var typ uint32
	if syscall.RegQueryValueEx(k, nil, nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &n) != nil {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func isInstalled() bool {
	if !strings.EqualFold(registeredPath(), installedDLL()) {
		return false
	}
	cur, err := os.ReadFile(installedDLL())
	return err == nil && bytes.Equal(cur, vcamDLL)
}

// runElevated re-launches this exe as admin with args and waits for it.
func runElevated(args string) error {
	exe, _ := os.Executable()
	sei := shellExecuteInfo{
		Mask:       0x00000040, // SEE_MASK_NOCLOSEPROCESS
		Verb:       u16("runas"),
		File:       u16(exe),
		Parameters: u16(args),
		Show:       0,
	}
	sei.Size = uint32(unsafe.Sizeof(sei))
	if r, _, err := pShellExecuteExW.Call(uintptr(unsafe.Pointer(&sei))); r == 0 {
		return fmt.Errorf("%s (%v)", T("noAdmin"), err)
	}
	defer syscall.CloseHandle(syscall.Handle(sei.Process))
	pWaitForSingleObject.Call(sei.Process, 0xFFFFFFFF)
	var code uint32
	pGetExitCodeProcess.Call(sei.Process, uintptr(unsafe.Pointer(&code)))
	if code != 0 {
		return errors.New(T("installFail", code, filepath.Join(installDir(), "install.log")))
	}
	return nil
}

// doInstall runs elevated: copies the DLL and registers the COM class.
func doInstall() error {
	dir := installDir()
	logs := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		return err
	}
	// Frame Server runs as LOCAL SERVICE (S-1-5-19): read the DLL, write logs.
	exec.Command("icacls", dir, "/grant", "*S-1-5-19:(OI)(CI)RX").Run()
	exec.Command("icacls", logs, "/grant", "*S-1-5-19:(OI)(CI)M").Run()

	// clean up DLLs left over from previous updates
	old, _ := filepath.Glob(filepath.Join(dir, dllName+".old*"))
	for _, f := range old {
		os.Remove(f)
	}
	target := installedDLL()
	if err := os.WriteFile(target, vcamDLL, 0o644); err != nil {
		// DLL is loaded by Frame Server; a loaded DLL can still be renamed.
		if rerr := os.Rename(target, fmt.Sprintf("%s.old%d", target, time.Now().UnixNano())); rerr != nil {
			return fmt.Errorf("write dll: %v", err)
		}
		if err := os.WriteFile(target, vcamDLL, 0o644); err != nil {
			return err
		}
	}
	out, err := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "regsvr32.exe"), "/s", target).CombinedOutput()
	if err != nil {
		return fmt.Errorf("regsvr32: %v %s", err, out)
	}
	return nil
}

func doUninstall() error {
	target := installedDLL()
	exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "regsvr32.exe"), "/s", "/u", target).Run()
	files, _ := filepath.Glob(filepath.Join(installDir(), dllName+"*"))
	for _, f := range files {
		os.Remove(f)
	}
	return nil
}

func ensureInstalled() error {
	if isInstalled() {
		return nil
	}
	if err := runElevated("-install"); err != nil {
		return err
	}
	if !isInstalled() {
		return errors.New(T("notReg"))
	}
	return nil
}

type virtualCamera struct{ obj uintptr }

func (v *virtualCamera) call(index int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(unsafe.Pointer(v.obj))
	fn := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(index)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{v.obj}, args...)...)
	return r
}

// IMFVirtualCamera vtable: IUnknown(0-2) + IMFAttributes(3-32) + own methods.
const (
	vcRelease  = 2
	vcStart    = 36
	vcRemove   = 38
	vcShutdown = 43
)

func hrErr(what string, hr uintptr) error {
	return fmt.Errorf("%s: HRESULT 0x%08X", what, uint32(hr))
}

// createVirtualCamera must be called on a COM-initialized thread.
func createVirtualCamera() (*virtualCamera, error) {
	if err := pMFCreateVirtualCamera.Find(); err != nil {
		return nil, errors.New(T("needWin11"))
	}
	if hr, _, _ := pMFStartup.Call(0x00020070, 0); int32(hr) < 0 {
		return nil, hrErr("MFStartup", hr)
	}
	var obj uintptr
	hr, _, _ := pMFCreateVirtualCamera.Call(
		0, // MFVirtualCameraType_SoftwareCameraSource
		0, // MFVirtualCameraLifetime_Session: exists while this app runs
		0, // MFVirtualCameraAccess_CurrentUser
		uintptr(unsafe.Pointer(u16(cameraName))),
		uintptr(unsafe.Pointer(u16(clsidStr))),
		0, 0,
		uintptr(unsafe.Pointer(&obj)))
	if int32(hr) < 0 {
		return nil, hrErr("MFCreateVirtualCamera", hr)
	}
	v := &virtualCamera{obj}
	if hr := v.call(vcStart, 0); int32(hr) < 0 {
		v.call(vcRelease)
		return nil, hrErr("IMFVirtualCamera.Start", hr)
	}
	return v, nil
}

func (v *virtualCamera) close() {
	if v == nil || v.obj == 0 {
		return
	}
	v.call(vcRemove)
	v.call(vcShutdown)
	v.call(vcRelease)
	v.obj = 0
}
