package main

import (
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	mfplat   = syscall.NewLazyDLL("mfplat.dll")
	mfsensor = syscall.NewLazyDLL("mfsensorgroup.dll")

	pRegisterClassExW       = user32.NewProc("RegisterClassExW")
	pCreateWindowExW        = user32.NewProc("CreateWindowExW")
	pDefWindowProcW         = user32.NewProc("DefWindowProcW")
	pGetMessageW            = user32.NewProc("GetMessageW")
	pTranslateMessage       = user32.NewProc("TranslateMessage")
	pDispatchMessageW       = user32.NewProc("DispatchMessageW")
	pPostQuitMessage        = user32.NewProc("PostQuitMessage")
	pPostMessageW           = user32.NewProc("PostMessageW")
	pDestroyWindow          = user32.NewProc("DestroyWindow")
	pCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	pAppendMenuW            = user32.NewProc("AppendMenuW")
	pTrackPopupMenu         = user32.NewProc("TrackPopupMenu")
	pDestroyMenu            = user32.NewProc("DestroyMenu")
	pGetCursorPos           = user32.NewProc("GetCursorPos")
	pSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	pSetTimer               = user32.NewProc("SetTimer")
	pRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	pCreateIconIndirect     = user32.NewProc("CreateIconIndirect")
	pDestroyIcon            = user32.NewProc("DestroyIcon")
	pGetDC                  = user32.NewProc("GetDC")
	pLoadIconW              = user32.NewProc("LoadIconW")
	pGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	pReleaseDC              = user32.NewProc("ReleaseDC")
	pMessageBoxW            = user32.NewProc("MessageBoxW")
	pOpenClipboard          = user32.NewProc("OpenClipboard")
	pEmptyClipboard         = user32.NewProc("EmptyClipboard")
	pSetClipboardData       = user32.NewProc("SetClipboardData")
	pCloseClipboard         = user32.NewProc("CloseClipboard")

	pGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	pCreateMutexW        = kernel32.NewProc("CreateMutexW")
	pGlobalAlloc         = kernel32.NewProc("GlobalAlloc")
	pGlobalLock          = kernel32.NewProc("GlobalLock")
	pGlobalUnlock        = kernel32.NewProc("GlobalUnlock")
	pWaitForSingleObject = kernel32.NewProc("WaitForSingleObject")
	pGetExitCodeProcess  = kernel32.NewProc("GetExitCodeProcess")

	pShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	pShellExecuteExW  = shell32.NewProc("ShellExecuteExW")
	pShellExecuteW    = shell32.NewProc("ShellExecuteW")

	pCreateDIBSection = gdi32.NewProc("CreateDIBSection")
	pCreateBitmap     = gdi32.NewProc("CreateBitmap")
	pDeleteObject     = gdi32.NewProc("DeleteObject")

	pCoInitializeEx = ole32.NewProc("CoInitializeEx")

	pRegSetValueExW = advapi32.NewProc("RegSetValueExW")
	pRegDeleteValue = advapi32.NewProc("RegDeleteValueW")
	pRegCreateKeyEx = advapi32.NewProc("RegCreateKeyExW")

	pMFStartup             = mfplat.NewProc("MFStartup")
	pMFCreateVirtualCamera = mfsensor.NewProc("MFCreateVirtualCamera")
)

const (
	wmDestroy     = 0x0002
	wmCommand     = 0x0111
	wmTimer       = 0x0113
	wmClose       = 0x0010
	wmLButtonUp   = 0x0202
	wmRButtonUp   = 0x0205
	wmApp         = 0x8000
	wmTrayMsg     = wmApp + 1
	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nifMessage    = 0x01
	nifIcon       = 0x02
	nifTip        = 0x04
	nifInfo       = 0x10
	niifInfo      = 0x01
	niifWarning   = 0x02
	niifError     = 0x03
	mfString      = 0x0000
	mfGrayed      = 0x0001
	mfChecked     = 0x0008
	mfSeparator   = 0x0800
	tpmReturnCmd  = 0x0100
	tpmRightBtn   = 0x0002
	tpmBottomAlgn = 0x0020
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type msgT struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	PtX, PtY int32
	LPrivate uint32
}

type notifyIconData struct {
	Size            uint32
	Wnd             uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUIDItem        [16]byte
	BalloonIcon     uintptr
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type iconInfo struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	Mask     uintptr
	Color    uintptr
}

type shellExecuteInfo struct {
	Size       uint32
	Mask       uint32
	Hwnd       uintptr
	Verb       *uint16
	File       *uint16
	Parameters *uint16
	Directory  *uint16
	Show       int32
	InstApp    uintptr
	IDList     uintptr
	Class      *uint16
	KeyClass   uintptr
	HotKey     uint32
	IconOrMon  uintptr
	Process    uintptr
}

func u16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func copyU16(dst []uint16, s string) {
	u, _ := syscall.UTF16FromString(s)
	if len(u) > len(dst) {
		u = u[:len(dst)]
		u[len(u)-1] = 0
	}
	copy(dst, u)
}

func messageBox(text, title string, flags uintptr) {
	pMessageBoxW.Call(0, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16(title))), flags)
}

func setClipboard(hwnd uintptr, s string) bool {
	u, _ := syscall.UTF16FromString(s)
	if r, _, _ := pOpenClipboard.Call(hwnd); r == 0 {
		return false
	}
	defer pCloseClipboard.Call()
	pEmptyClipboard.Call()
	h, _, _ := pGlobalAlloc.Call(0x0002 /*GMEM_MOVEABLE*/, uintptr(len(u)*2))
	if h == 0 {
		return false
	}
	p, _, _ := pGlobalLock.Call(h)
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(u)), u)
	pGlobalUnlock.Call(h)
	r, _, _ := pSetClipboardData.Call(13 /*CF_UNICODETEXT*/, h)
	return r != 0
}

// Run-at-login via HKCU\...\Run
const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func autostartEnabled() bool {
	var k syscall.Handle
	if syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, u16(runKey), 0, syscall.KEY_READ, &k) != nil {
		return false
	}
	defer syscall.RegCloseKey(k)
	var typ, n uint32
	return syscall.RegQueryValueEx(k, u16("PhoneCam"), nil, &typ, nil, &n) == nil
}

func setAutostart(on bool, exe string) {
	var k syscall.Handle
	if syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, u16(runKey), 0, syscall.KEY_WRITE, &k) != nil {
		return
	}
	defer syscall.RegCloseKey(k)
	if on {
		v, _ := syscall.UTF16FromString(`"` + exe + `"`)
		pRegSetValueExW.Call(uintptr(k), uintptr(unsafe.Pointer(u16("PhoneCam"))), 0, syscall.REG_SZ,
			uintptr(unsafe.Pointer(&v[0])), uintptr(len(v)*2))
	} else {
		pRegDeleteValue.Call(uintptr(k), uintptr(unsafe.Pointer(u16("PhoneCam"))))
	}
}
