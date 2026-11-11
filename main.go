// PhoneCam: use a phone's camera as a Windows webcam.
//
// The phone opens https://<pc-ip>:8443, grants camera access, and streams JPEG
// frames. They are converted to NV12 and handed to a Media Foundation virtual
// camera ("PhoneCam") that every Windows app can select. Lives in the tray.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

//go:embed assets/phonecam_vcam.dll
var vcamDLL []byte

//go:embed web/index.html
var indexHTML []byte

var version = "dev" // set with -ldflags "-X main.version=..."

func init() { runtime.LockOSThread() } // Win32 UI + COM stay on the main thread

func main() {
	port := flag.Int("port", 8443, "HTTPS port for the phone")
	install := flag.Bool("install", false, "internal: install driver (elevated)")
	uninstall := flag.Bool("uninstall", false, "internal: remove driver (elevated)")
	flag.Parse()

	if *install || *uninstall {
		os.MkdirAll(installDir(), 0o755)
		if lf, err := os.Create(filepath.Join(installDir(), "install.log")); err == nil {
			log.SetOutput(lf)
		}
		var err error
		if *install {
			err = doInstall()
		} else {
			err = doUninstall()
		}
		if err != nil {
			log.Printf("error: %v", err)
			os.Exit(1)
		}
		log.Printf("ok")
		return
	}

	if f, err := os.OpenFile(filepath.Join(dataDir(), "phonecam.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600); err == nil {
		log.SetOutput(f)
	}
	log.Printf("PhoneCam %s", version)

	// single instance
	_, _, merr := pCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(u16(`Local\PhoneCamTray`))))
	if merr == syscall.Errno(183) { // ERROR_ALREADY_EXISTS
		messageBox(T("already"), "PhoneCam", 0x40)
		return
	}

	pCoInitializeEx.Call(0, 0x2) // COINIT_APARTMENTTHREADED

	exe, _ := os.Executable()
	h := newHub()
	app = &trayApp{hub: h, port: *port, ips: localIPs(), exe: exe}
	if err := app.init(); err != nil {
		messageBox(T("trayFail", err), "PhoneCam", 0x10)
		return
	}

	if err := h.serveFeed(); err != nil {
		app.srvErr = fmt.Errorf("%s: %v", feedAddr, err)
	}
	if err := startHTTPS(*port, h); err != nil && app.srvErr == nil {
		app.srvErr = fmt.Errorf("port %d: %v", *port, err)
	}

	var cam *virtualCamera
	if err := ensureInstalled(); err != nil {
		app.camErr = err
	} else if cam, err = createVirtualCamera(); err != nil {
		app.camErr = err
	}
	app.onExit = func() { cam.close() }
	log.Printf("ips=%v srvErr=%v camErr=%v", app.ips, app.srvErr, app.camErr)

	switch {
	case app.srvErr != nil:
		app.balloon(T("srvFail"), app.srvErr.Error(), niifError)
	case app.camErr != nil:
		app.balloon(T("camFail"), app.camErr.Error(), niifError)
	default:
		app.balloon(T("ready"), T("readyBody", app.mainURL()), niifInfo)
	}
	app.refresh()
	app.run()
	cam.close()
}
