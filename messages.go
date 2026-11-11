package main

import "fmt"

// UI strings.
var messages = map[string]string{
	"already":      "PhoneCam is already running (see the system tray).",
	"trayFail":     "Could not create the tray icon: %v",
	"ready":        "PhoneCam is ready",
	"readyBody":    "Open on your phone: %s\nThen pick \"PhoneCam\" as the camera on this PC.",
	"srvFail":      "PhoneCam could not start",
	"camFail":      "Could not create the virtual camera",
	"srvErr":       "Server error: %v",
	"camErr":       "Virtual camera error: %v",
	"phoneOK":      "Phone connected · %d fps",
	"phoneWait":    "Waiting for phone",
	"inUse":        " · camera in use",
	"openOnPhone":  "Open on your phone (click = copy):",
	"noNetwork":    "No network connection found",
	"testCam":      "Test in the Camera app",
	"autostart":    "Start with Windows",
	"logs":         "Open log folder",
	"website":      "GitHub page",
	"uninstall":    "Remove virtual camera and exit",
	"exit":         "Exit",
	"copied":       "Address copied",
	"copiedBody":   "%s\nPaste it into your phone's browser. On the certificate warning choose \"Advanced → Proceed\".",
	"netChanged":   "Network changed",
	"netBody":      "New address: %s",
	"uninstallErr": "Uninstall failed: %v",
	"noAdmin":      "administrator permission was not granted",
	"installFail":  "install failed (code %d), see: %s",
	"notReg":       "the virtual camera driver could not be registered",
	"needWin11":    "this Windows version does not support virtual cameras (Windows 11 required)",
}

// T returns the UI string for key, formatted with args.
func T(key string, args ...any) string {
	s := messages[key]
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}
