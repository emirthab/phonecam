<p align="center">
  <img src="docs/logo.png" width="128" alt="PhoneCam logo">
</p>

<h1 align="center">PhoneCam</h1>

<p align="center">
  <b>Use your iPhone or Android phone as a webcam on Windows 11 — over Wi-Fi, with no app on the phone.</b><br>
  Open a link in the phone's browser, allow the camera, done. Every Windows app sees a real camera called <b>PhoneCam</b>.
</p>

<p align="center">
  <a href="https://github.com/emirthab/phonecam/releases/latest"><img src="https://img.shields.io/github/v/release/emirthab/phonecam?label=download&color=06b6d4" alt="Latest release"></a>
  <img src="https://img.shields.io/badge/Windows-11-0078D6?logo=windows11&logoColor=white" alt="Windows 11">
  <img src="https://img.shields.io/badge/phone-iOS%20%7C%20Android-6366f1" alt="iOS and Android">
  <a href="LICENSE"><img src="https://img.shields.io/github/license/emirthab/phonecam?color=22c55e" alt="MIT license"></a>
</p>

---

Your PC has no webcam but you need one for a video call, a browser-based face/ID check or a quick recording? PhoneCam turns the phone you already have into a **system-wide virtual webcam**:

- **Nothing to install on the phone.** It's just a web page served by your PC.
- **A real Windows camera.** It uses the Windows 11 virtual camera API (`MFCreateVirtualCamera`), so browsers, video-call, streaming and recording apps all list it like a USB webcam.
- **Tray-only.** One small `.exe` with no window and no setup wizard. It lives in the system tray.
- **Local only.** Video goes from the phone to the PC over your own network. No cloud, no account, no tracking.

## Download

**[⬇ Download phonecam.exe](https://github.com/emirthab/phonecam/releases/latest/download/phonecam.exe)** (Windows 11, 64-bit)

## Quick start

1. **Run `phonecam.exe` on the PC.** The first time, Windows asks for administrator permission (UAC) once, to register the virtual camera driver. A camera icon appears in the system tray.
2. **On your phone**, connect to the **same Wi-Fi/router** as the PC and open the address shown in the tray notification, e.g. `https://192.168.1.50:8443`.
3. You'll see a **certificate warning**. This is expected (see *Why the certificate warning?* under [Troubleshooting](#troubleshooting)).
   - Android: **Advanced → Proceed**
   - iPhone: **Show Details → visit this website**
4. Tap **Start Camera** and allow camera access.
5. On the PC, pick **PhoneCam** as the camera in any app.

> Keep the phone's screen on and the page open. Mobile browsers pause the camera when the tab goes to the background.

## Features

| | |
|---|---|
| 📱 **Any phone** | Works in the default browser on iPhone and Android. |
| 🎥 **System-wide camera** | 1280×720 @ 30 fps, NV12, visible to every app that uses Media Foundation (all modern apps and browsers). |
| 🔄 **Front / back camera** | Switch from the phone page. |
| 🖼️ **Fit or Fill** | Letterbox a portrait phone image, or crop it to fill the 16:9 frame. |
| 🎚️ **Quality presets** | High / Normal / Low JPEG quality for weak Wi-Fi. |
| 🟢 **Tray status** | The icon dot turns green when the phone is streaming, amber while waiting, red on errors. Hover for fps. |
| 🌐 **Network aware** | Picks up IP changes (cable/Wi-Fi) automatically and tells you the new address. |
| 🚀 **Start with Windows** | One click in the tray menu. |

### Tray menu

- Current status (phone connected · fps · camera in use)
- Phone addresses, click one to copy it
- **Test in the Camera app**
- **Start with Windows**
- **Open log folder**
- **Remove virtual camera and exit** (uninstall)
- **Exit**

## How it works

```
 Phone browser                         Windows PC
┌──────────────┐  HTTPS (JPEG frames) ┌──────────────────────┐ loopback  ┌──────────────────────────┐
│ getUserMedia │ ───────────────────▶ │ phonecam.exe (tray)  │ ────────▶ │ Frame Server service     │
│ canvas→JPEG  │      Wi-Fi / LAN     │ JPEG → NV12 1280×720 │  raw NV12 │ phonecam_vcam.dll        │
└──────────────┘                      └──────────────────────┘           │ (MF virtual camera source)│
                                                                         └────────────┬─────────────┘
                                                                                      ▼
                                                          Browsers · video calls · streaming · Camera app
```

- `phonecam.exe` (Go) serves the phone page over HTTPS, receives JPEG frames, converts them to NV12, and creates the virtual camera with `MFCreateVirtualCamera`.
- `phonecam_vcam.dll` (C++, embedded in the exe) is a Media Foundation media source. The Windows Camera Frame Server loads it and pulls frames from the tray app over `127.0.0.1:47811`.
- On first run the DLL is copied to `%ProgramData%\PhoneCam` and registered. That folder is readable by the Frame Server service, which a user profile folder is not.
- The camera exists only while PhoneCam is running.

## Requirements

- **Windows 11** (64-bit). The virtual camera API does not exist on Windows 10.
- PC and phone on the **same local network**.
- A phone browser with camera support (any recent iPhone or Android browser).

## Troubleshooting

<details>
<summary><b>The phone can't open the page / times out</b></summary>

- Make sure the address starts with **`https://`**, not `http://`.
- The phone must be on the **same network** as the PC. Compare the first three numbers of the IPs: the phone's Wi-Fi IP (phone settings) and the address in the tray menu, e.g. `192.168.1.x` on both. If they differ (e.g. `192.168.1.x` vs `192.168.0.x`), the PC and phone are on different routers or subnets. Common cause: a PC plugged into the ISP modem while the phone uses a second Wi-Fi router, or an extra cable between modem and router that merges two networks.
- Guest Wi-Fi networks and "AP/client isolation" block devices from talking to each other.
- Windows Firewall: allow PhoneCam on **Private networks** when Windows asks. To add the rule manually, run in an elevated terminal:
  ```
  netsh advfirewall firewall add rule name="PhoneCam" dir=in action=allow protocol=TCP localport=8443 profile=private
  ```
- If the network is marked **Public** in Windows, switch it to Private (Settings → Network & internet → your network).
</details>

<details>
<summary><b>Why the certificate warning?</b></summary>

Browsers only allow camera access on secure (HTTPS) pages. PhoneCam generates its own certificate on first run and stores it in `%APPDATA%\PhoneCam`. The connection is still encrypted, but no public authority signed the certificate, so the phone asks you to confirm once.
</details>

<details>
<summary><b>"PhoneCam" doesn't show up as a camera</b></summary>

- You need Windows 11. The tray icon turns red with a message if the virtual camera can't be created.
- Accept the UAC prompt on first run. Without it, the driver can't be registered.
- Restart the app that should see the camera. Some apps only list cameras at startup.
- Logs: `%APPDATA%\PhoneCam\phonecam.log` and `%ProgramData%\PhoneCam\logs\vcam.log`.
</details>

<details>
<summary><b>Picture is grey with a moving bar</b></summary>

That's the "waiting for phone" placeholder. The camera is working, but no frames have arrived in the last 3 seconds. Open the page on the phone and tap **Start Camera**.
</details>

<details>
<summary><b>Windows SmartScreen warns about the exe</b></summary>

The exe isn't code-signed (signing certificates cost money). Click **More info → Run anyway**, or [build it yourself](#build-from-source).
</details>

<details>
<summary><b>Does it work with identity / face verification sites?</b></summary>

It delivers your real, live camera image, so it works anywhere a webcam is accepted. Some verification services detect and reject virtual cameras by policy. PhoneCam can't change that and doesn't try to hide itself.
</details>

## Security notes

- The phone page and frame upload are served on your LAN (port 8443). Anyone on the same network who knows the address could send images to the PhoneCam camera while it runs. Use it on networks you trust.
- The frame feed between the tray app and the driver only listens on `127.0.0.1`.
- Nothing is sent to the internet.

## Uninstall

Tray menu → **Remove virtual camera and exit**. This unregisters the driver and deletes `%ProgramData%\PhoneCam\phonecam_vcam.dll`. Then delete `phonecam.exe` and, optionally, `%APPDATA%\PhoneCam`.

## Build from source

Requires [Go](https://go.dev/dl/) 1.22+ and [MinGW-w64](https://winlibs.com/) (`g++`, `windres`) on `PATH`.

```powershell
git clone https://github.com/emirthab/phonecam
cd phonecam
.\build.ps1 -Version 1.0.0     # -> dist\phonecam.exe
```

| Path | What |
|---|---|
| `main.go`, `tray.go`, `win32.go` | Tray app (pure Win32 via `syscall`, no dependencies) |
| `server.go` | HTTPS server, self-signed certificate, LAN IP discovery |
| `frames.go` | JPEG → NV12 conversion and the loopback frame feed |
| `vcam.go` | Driver install/registration and `MFCreateVirtualCamera` |
| `vcam/vcam.cpp` | Media Foundation virtual camera source (DLL) |
| `web/index.html` | The phone page |
| `internal/icon` | Procedural app/tray icon (`go run ./tools/mkicon`) |
| `tools/captest` | Opens the PhoneCam camera like an app and prints frame stats |

No third-party Go modules. Standard library only.

## License

[MIT](LICENSE)

