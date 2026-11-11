package main

import (
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"slices"
	"syscall"
	"unsafe"

	"github.com/emirthab/phonecam/internal/icon"
)

const repoURL = "https://github.com/emirthab/phonecam"

const (
	cmdCopyBase  = 100
	cmdAutostart = 200
	cmdTestCam   = 201
	cmdLogs      = 202
	cmdUninstall = 203
	cmdExit      = 204
	cmdWebsite   = 205
)

type trayApp struct {
	hwnd           uintptr
	nid            notifyIconData
	icons          map[string]uintptr
	curIcon        string
	curTip         string
	taskbarCreated uintptr
	hub            *hub
	port           int
	ips            []string
	ticks          int
	camErr         error
	srvErr         error
	exe            string
	onExit         func()
}

var app *trayApp

func (a *trayApp) url(ip string) string { return fmt.Sprintf("https://%s:%d", ip, a.port) }

func (a *trayApp) mainURL() string {
	if len(a.ips) == 0 {
		return a.url("<PC-IP>")
	}
	return a.url(a.ips[0])
}

func wndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	a := app
	switch {
	case msg == wmTrayMsg:
		switch lparam & 0xffff {
		case wmLButtonUp, wmRButtonUp:
			a.showMenu()
		}
		return 0
	case msg == wmTimer:
		a.refresh()
		return 0
	case msg == wmCommand:
		a.command(int(wparam & 0xffff))
		return 0
	case msg == wmDestroy:
		pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&a.nid)))
		pPostQuitMessage.Call(0)
		return 0
	case a != nil && a.taskbarCreated != 0 && msg == a.taskbarCreated:
		// Explorer restarted: re-add the icon
		pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&a.nid)))
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func (a *trayApp) init() error {
	inst, _, _ := pGetModuleHandleW.Call(0)
	cls := u16("PhoneCamTrayWnd")
	appIcon, _, _ := pLoadIconW.Call(inst, 1) // icon resource from phonecam.rc
	wc := wndClassEx{WndProc: syscall.NewCallback(wndProc), Instance: inst, ClassName: cls, Icon: appIcon, IconSm: appIcon}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassEx: %v", err)
	}
	hwnd, _, err := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(u16("PhoneCam"))),
		0, 0, 0, 0, 0, 0, 0, inst, 0)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowEx: %v", err)
	}
	a.hwnd = hwnd
	a.taskbarCreated, _, _ = pRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(u16("TaskbarCreated"))))

	size, _, _ := pGetSystemMetrics.Call(49) // SM_CXSMICON, DPI aware via manifest
	if size < 16 {
		size = 16
	}
	a.icons = map[string]uintptr{
		"ok":   hicon(int(size), icon.StatusOK),
		"wait": hicon(int(size), icon.StatusWait),
		"err":  hicon(int(size), icon.StatusErr),
	}
	a.nid.Size = uint32(unsafe.Sizeof(a.nid))
	a.nid.Wnd = hwnd
	a.nid.ID = 1
	a.nid.Flags = nifMessage | nifIcon | nifTip
	a.nid.CallbackMessage = wmTrayMsg
	a.curIcon = "wait"
	a.nid.Icon = a.icons["wait"]
	copyU16(a.nid.Tip[:], "PhoneCam")
	if r, _, err := pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&a.nid))); r == 0 {
		return fmt.Errorf("Shell_NotifyIcon: %v", err)
	}
	pSetTimer.Call(hwnd, 1, 1000, 0)
	return nil
}

func (a *trayApp) balloon(title, text string, flags uint32) {
	n := a.nid
	n.Flags = nifInfo
	n.InfoFlags = flags
	copyU16(n.InfoTitle[:], title)
	copyU16(n.Info[:], text)
	pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&n)))
}

func (a *trayApp) statusText() (string, string) {
	if a.srvErr != nil {
		return "err", T("srvErr", a.srvErr)
	}
	if a.camErr != nil {
		return "err", T("camErr", a.camErr)
	}
	st := a.hub.status()
	use := ""
	if st.clients > 0 {
		use = T("inUse")
	}
	if st.phoneConnected {
		return "ok", T("phoneOK", st.fps) + use
	}
	return "wait", T("phoneWait") + use
}

// updateIPs re-reads the LAN addresses (cable/Wi-Fi changes) and tells the user.
func (a *trayApp) updateIPs() {
	ips := localIPs()
	if slices.Equal(ips, a.ips) {
		return
	}
	a.ips = ips
	if len(ips) > 0 {
		a.balloon(T("netChanged"), T("netBody", a.mainURL()), niifInfo)
	}
}

func (a *trayApp) refresh() {
	a.ticks++
	if a.ticks%5 == 0 {
		a.updateIPs()
	}
	icon, text := a.statusText()
	tip := "PhoneCam — " + text + "\n" + a.mainURL()
	if icon == a.curIcon && tip == a.curTip {
		return
	}
	a.curIcon, a.curTip = icon, tip
	a.nid.Flags = nifIcon | nifTip | nifMessage
	a.nid.Icon = a.icons[icon]
	a.nid.Tip = [128]uint16{}
	copyU16(a.nid.Tip[:], tip)
	pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&a.nid)))
}

func (a *trayApp) showMenu() {
	a.updateIPs()
	m, _, _ := pCreatePopupMenu.Call()
	defer pDestroyMenu.Call(m)
	add := func(flags uintptr, id int, text string) {
		pAppendMenuW.Call(m, flags, uintptr(id), uintptr(unsafe.Pointer(u16(text))))
	}
	_, status := a.statusText()
	add(mfString|mfGrayed, 0, "PhoneCam "+version)
	add(mfString|mfGrayed, 0, status)
	add(mfSeparator, 0, "")
	add(mfString|mfGrayed, 0, T("openOnPhone"))
	for i, ip := range a.ips {
		if i >= 50 {
			break
		}
		add(mfString, cmdCopyBase+i, "    "+a.url(ip))
	}
	if len(a.ips) == 0 {
		add(mfString|mfGrayed, 0, "    "+T("noNetwork"))
	}
	add(mfString, cmdTestCam, T("testCam"))
	add(mfSeparator, 0, "")
	var auto uintptr
	if autostartEnabled() {
		auto = mfChecked
	}
	add(mfString|auto, cmdAutostart, T("autostart"))
	add(mfString, cmdLogs, T("logs"))
	add(mfString, cmdWebsite, T("website"))
	add(mfString, cmdUninstall, T("uninstall"))
	add(mfSeparator, 0, "")
	add(mfString, cmdExit, T("exit"))

	var pt struct{ X, Y int32 }
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(a.hwnd)
	cmd, _, _ := pTrackPopupMenu.Call(m, tpmReturnCmd|tpmRightBtn|tpmBottomAlgn, uintptr(pt.X), uintptr(pt.Y), 0, a.hwnd, 0)
	pPostMessageW.Call(a.hwnd, 0, 0, 0)
	if cmd != 0 {
		a.command(int(cmd))
	}
}

func shellOpen(target string) {
	pShellExecuteW.Call(0, uintptr(unsafe.Pointer(u16("open"))), uintptr(unsafe.Pointer(u16(target))), 0, 0, 1)
}

func (a *trayApp) command(id int) {
	switch {
	case id >= cmdCopyBase && id < cmdCopyBase+len(a.ips):
		u := a.url(a.ips[id-cmdCopyBase])
		if setClipboard(a.hwnd, u) {
			a.balloon(T("copied"), T("copiedBody", u), niifInfo)
		}
	case id == cmdTestCam:
		shellOpen("microsoft.windows.camera:")
	case id == cmdWebsite:
		shellOpen(repoURL)
	case id == cmdAutostart:
		setAutostart(!autostartEnabled(), a.exe)
	case id == cmdLogs:
		exec.Command("explorer.exe", dataDir()).Start()
	case id == cmdUninstall:
		a.shutdown()
		if err := runElevated("-uninstall"); err != nil {
			messageBox(T("uninstallErr", err), "PhoneCam", 0x10)
		}
		setAutostart(false, a.exe)
		os.Exit(0)
	case id == cmdExit:
		a.shutdown()
		pDestroyWindow.Call(a.hwnd)
	}
}

func (a *trayApp) shutdown() {
	if a.onExit != nil {
		a.onExit()
		a.onExit = nil
	}
	pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&a.nid)))
}

func (a *trayApp) run() {
	var m msgT
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// hicon renders the app icon with a status dot into an HICON.
func hicon(size int, status color.RGBA) uintptr {
	img := icon.Render(size, status)
	bi := bitmapInfoHeader{Width: int32(size), Height: -int32(size), Planes: 1, BitCount: 32}
	bi.Size = uint32(unsafe.Sizeof(bi))
	hdc, _, _ := pGetDC.Call(0)
	var bits unsafe.Pointer
	colorBmp, _, _ := pCreateDIBSection.Call(hdc, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	pReleaseDC.Call(0, hdc)
	if colorBmp == 0 {
		return 0
	}
	pix := unsafe.Slice((*byte)(bits), size*size*4)
	for i := 0; i < size*size; i++ {
		r, g, b, al := img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3]
		if al > 0 && al < 255 { // image.RGBA is premultiplied; icons want straight alpha
			r = uint8(uint16(r) * 255 / uint16(al))
			g = uint8(uint16(g) * 255 / uint16(al))
			b = uint8(uint16(b) * 255 / uint16(al))
		}
		pix[i*4], pix[i*4+1], pix[i*4+2], pix[i*4+3] = b, g, r, al
	}
	maskBits := make([]byte, ((size+15)/16*2)*size)
	mask, _, _ := pCreateBitmap.Call(uintptr(size), uintptr(size), 1, 1, uintptr(unsafe.Pointer(&maskBits[0])))
	ii := iconInfo{FIcon: 1, Mask: mask, Color: colorBmp}
	h, _, _ := pCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	pDeleteObject.Call(colorBmp)
	pDeleteObject.Call(mask)
	return h
}
