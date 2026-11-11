// PhoneCam virtual camera media source (Windows 11 MFCreateVirtualCamera).
//
// The Windows Camera Frame Server loads this DLL and asks it for a media
// source. Frames are pulled from the PhoneCam tray app over a loopback TCP
// connection (127.0.0.1:47811) as raw NV12 1280x720.

#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#define WINVER 0x0A00
#define _WIN32_WINNT 0x0A00
#define NTDDI_VERSION 0x0A00000C
#include <winsock2.h>
#include <ws2tcpip.h>
#include <windows.h>
#include <initguid.h>
#include <mfapi.h>
#include <mfidl.h>
#include <mferror.h>
#include <deque>
#include <cstdio>
#include <cstdarg>
#include <cstring>
#include <cstdint>

#define FRAME_W 1280
#define FRAME_H 720
#define FRAME_SIZE (FRAME_W * FRAME_H * 3 / 2)
#define FRAME_DURATION 333333LL // 100ns units, 30 fps
#define FEED_PORT 47811
#define STALE_MS 3000

// {6E0F4A6B-8F2D-4C55-9A3E-5B712C84D107}
static const GUID CLSID_PhoneCam = {0x6e0f4a6b, 0x8f2d, 0x4c55, {0x9a, 0x3e, 0x5b, 0x71, 0x2c, 0x84, 0xd1, 0x07}};
static const wchar_t *CLSID_PhoneCam_Str = L"{6E0F4A6B-8F2D-4C55-9A3E-5B712C84D107}";

// Declarations missing from MinGW headers.
static const GUID IID_IMFMediaStream2_ = {0xc5bc37d6, 0x75c7, 0x46a1, {0xa1, 0x32, 0x81, 0xb5, 0xf7, 0x23, 0xc2, 0x0f}};
static const GUID IID_IKsControl_ = {0x28f54685, 0x06fd, 0x11d2, {0xb2, 0x7a, 0x00, 0xa0, 0xc9, 0x22, 0x31, 0x96}};
static const GUID MF_DEVICESTREAM_STREAM_CATEGORY_ = {0x2939e7b8, 0xa62e, 0x4579, {0xb6, 0x74, 0xd4, 0x07, 0x3d, 0xfa, 0xbb, 0xba}};
static const GUID MF_DEVICESTREAM_STREAM_ID_ = {0x11bd5120, 0xd124, 0x446b, {0x88, 0xe6, 0x17, 0x06, 0x02, 0x57, 0xff, 0xf9}};
static const GUID MF_DEVICESTREAM_FRAMESERVER_SHARED_ = {0x1cb378e9, 0xb279, 0x41d4, {0xaf, 0x97, 0x34, 0xa2, 0x43, 0xe6, 0x83, 0x20}};
static const GUID MF_DEVICESTREAM_ATTRIBUTE_FRAMESOURCE_TYPES_ = {0x17145fd1, 0x1b2b, 0x423c, {0x80, 0x01, 0x2b, 0x68, 0x33, 0xed, 0x35, 0x88}};
static const GUID PINNAME_VIDEO_CAPTURE_ = {0xfb6c4281, 0x0353, 0x11d1, {0x90, 0x5f, 0x00, 0x00, 0xc0, 0xcc, 0x16, 0xba}};
static const UINT32 MFFrameSourceTypes_Color_ = 0x0001;
#ifndef GUID_NULL
static const GUID GUID_NULL_ = {0, 0, 0, {0, 0, 0, 0, 0, 0, 0, 0}};
#define GUID_NULL GUID_NULL_
#endif

struct IMFMediaStream2X : public IMFMediaStream {
    virtual HRESULT STDMETHODCALLTYPE SetStreamState(MF_STREAM_STATE value) = 0;
    virtual HRESULT STDMETHODCALLTYPE GetStreamState(MF_STREAM_STATE *value) = 0;
};

struct IKsControlX : public IUnknown {
    virtual HRESULT STDMETHODCALLTYPE KsProperty(void *p, ULONG pl, void *d, ULONG dl, ULONG *ret) = 0;
    virtual HRESULT STDMETHODCALLTYPE KsMethod(void *m, ULONG ml, void *d, ULONG dl, ULONG *ret) = 0;
    virtual HRESULT STDMETHODCALLTYPE KsEvent(void *e, ULONG el, void *d, ULONG dl, ULONG *ret) = 0;
};

static HMODULE g_module;
static LONG g_objects;

template <class T> static void SafeRelease(T *&p) {
    if (p) {
        p->Release();
        p = nullptr;
    }
}

// ---------------------------------------------------------------- logging

static SRWLOCK g_logLock = SRWLOCK_INIT;

static void Log(const char *fmt, ...) {
    char msg[1024];
    va_list ap;
    va_start(ap, fmt);
    vsnprintf(msg, sizeof msg, fmt, ap);
    va_end(ap);
    OutputDebugStringA("[PhoneCam] ");
    OutputDebugStringA(msg);
    OutputDebugStringA("\n");

    char path[MAX_PATH];
    if (!ExpandEnvironmentStringsA("%ProgramData%\\PhoneCam\\logs\\vcam.log", path, MAX_PATH)) return;
    AcquireSRWLockExclusive(&g_logLock);
    FILE *f = fopen(path, "ab");
    if (f) {
        fseek(f, 0, SEEK_END);
        if (ftell(f) > 1024 * 1024) {
            fclose(f);
            f = fopen(path, "wb");
        }
    }
    if (f) {
        SYSTEMTIME st;
        GetLocalTime(&st);
        fprintf(f, "%02d:%02d:%02d.%03d [%lu] %s\r\n", st.wHour, st.wMinute, st.wSecond, st.wMilliseconds,
                GetCurrentProcessId(), msg);
        fclose(f);
    }
    ReleaseSRWLockExclusive(&g_logLock);
}

// ------------------------------------------------------------- frame feed

static SRWLOCK g_feedLock = SRWLOCK_INIT;
static BYTE *g_frame;          // latest frame, guarded by g_feedLock
static ULONGLONG g_frameTick;  // GetTickCount64 of latest frame
static LONG g_feedStarted;

static bool RecvAll(SOCKET s, char *p, int n) {
    while (n > 0) {
        int r = recv(s, p, n, 0);
        if (r <= 0) return false;
        p += r;
        n -= r;
    }
    return true;
}

static DWORD WINAPI FeedThread(LPVOID) {
    WSADATA wsa;
    WSAStartup(MAKEWORD(2, 2), &wsa);
    BYTE *buf = (BYTE *)malloc(FRAME_SIZE);
    bool failLogged = false;
    for (;;) {
        SOCKET s = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
        if (s == INVALID_SOCKET) {
            Sleep(1000);
            continue;
        }
        sockaddr_in a = {};
        a.sin_family = AF_INET;
        a.sin_port = htons(FEED_PORT);
        a.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
        if (connect(s, (sockaddr *)&a, sizeof a) != 0) {
            if (!failLogged) Log("feed: connect failed (%d), tray app running?", WSAGetLastError());
            failLogged = true;
            closesocket(s);
            Sleep(500);
            continue;
        }
        failLogged = false;
        Log("feed: connected");
        int one = 1;
        setsockopt(s, IPPROTO_TCP, TCP_NODELAY, (char *)&one, sizeof one);
        int rcv = 4 * 1024 * 1024;
        setsockopt(s, SOL_SOCKET, SO_RCVBUF, (char *)&rcv, sizeof rcv);
        for (;;) {
            uint32_t hdr[4];
            if (!RecvAll(s, (char *)hdr, sizeof hdr)) break;
            // "PCAM", width, height, payload size (little endian)
            if (hdr[0] != 0x4D414350 || hdr[1] != FRAME_W || hdr[2] != FRAME_H || hdr[3] != FRAME_SIZE) {
                Log("feed: bad header");
                break;
            }
            if (!RecvAll(s, (char *)buf, FRAME_SIZE)) break;
            AcquireSRWLockExclusive(&g_feedLock);
            BYTE *t = g_frame;
            g_frame = buf;
            buf = t ? t : (BYTE *)malloc(FRAME_SIZE);
            g_frameTick = GetTickCount64();
            ReleaseSRWLockExclusive(&g_feedLock);
        }
        Log("feed: disconnected");
        closesocket(s);
        Sleep(300);
    }
    return 0;
}

static void EnsureFeed() {
    if (InterlockedCompareExchange(&g_feedStarted, 1, 0) == 0) {
        HANDLE h = CreateThread(nullptr, 0, FeedThread, nullptr, 0, nullptr);
        if (h) CloseHandle(h);
    }
}

// Fills an NV12 frame: the latest phone frame, or a dark placeholder.
static void FillFrame(BYTE *dst) {
    bool ok = false;
    AcquireSRWLockShared(&g_feedLock);
    if (g_frame && GetTickCount64() - g_frameTick < STALE_MS) {
        memcpy(dst, g_frame, FRAME_SIZE);
        ok = true;
    }
    ReleaseSRWLockShared(&g_feedLock);
    if (ok) return;

    // Placeholder: dark gray with a slowly moving bar so it is clearly "live".
    memset(dst, 40, FRAME_W * FRAME_H);
    memset(dst + FRAME_W * FRAME_H, 128, FRAME_W * FRAME_H / 2);
    int bar = (int)((GetTickCount64() / 8) % FRAME_W);
    for (int y = FRAME_H - 12; y < FRAME_H - 4; y++)
        for (int x = bar; x < bar + 80 && x < FRAME_W; x++) dst[y * FRAME_W + x] = 120;
}

// ------------------------------------------------------------------ stream

static HRESULT SetStreamAttrs(IMFAttributes *a) {
    HRESULT hr = a->SetGUID(MF_DEVICESTREAM_STREAM_CATEGORY_, PINNAME_VIDEO_CAPTURE_);
    if (SUCCEEDED(hr)) hr = a->SetUINT32(MF_DEVICESTREAM_STREAM_ID_, 0);
    if (SUCCEEDED(hr)) hr = a->SetUINT32(MF_DEVICESTREAM_FRAMESERVER_SHARED_, 1);
    if (SUCCEEDED(hr)) hr = a->SetUINT32(MF_DEVICESTREAM_ATTRIBUTE_FRAMESOURCE_TYPES_, MFFrameSourceTypes_Color_);
    return hr;
}

class Stream : public IMFMediaStream2X, public IKsControlX {
    LONG ref = 1;
    CRITICAL_SECTION cs;
    IMFMediaSource *source = nullptr;
    IMFMediaEventQueue *queue = nullptr;
    IMFStreamDescriptor *sd = nullptr;
    IMFAttributes *attrs = nullptr;
    MF_STREAM_STATE state = MF_STREAM_STATE_STOPPED;
    bool isShutdown = false;
    std::deque<IUnknown *> tokens; // nullptr entries allowed
    HANDLE wake = nullptr;
    volatile LONG stopWorker = 0;

    ~Stream() {
        for (auto t : tokens) if (t) t->Release();
        SafeRelease(queue);
        SafeRelease(sd);
        SafeRelease(attrs);
        SafeRelease(source);
        if (wake) CloseHandle(wake);
        DeleteCriticalSection(&cs);
        InterlockedDecrement(&g_objects);
    }

    static DWORD WINAPI WorkerThunk(LPVOID p) {
        Stream *s = (Stream *)p;
        s->Worker();
        s->Release();
        return 0;
    }

    void Worker() {
        LONGLONG nextDue = 0;
        while (!stopWorker) {
            IUnknown *token = nullptr;
            bool have = false;
            EnterCriticalSection(&cs);
            if (!isShutdown && state == MF_STREAM_STATE_RUNNING && !tokens.empty()) {
                token = tokens.front();
                tokens.pop_front();
                have = true;
            }
            LeaveCriticalSection(&cs);
            if (!have) {
                WaitForSingleObject(wake, 200);
                continue;
            }
            LONGLONG now = MFGetSystemTime();
            if (now < nextDue) {
                Sleep((DWORD)((nextDue - now) / 10000));
                now = MFGetSystemTime();
            }
            nextDue = (nextDue + FRAME_DURATION > now) ? nextDue + FRAME_DURATION : now + FRAME_DURATION;
            Deliver(token);
            if (token) token->Release();
        }
    }

    void Deliver(IUnknown *token) {
        IMFMediaBuffer *buf = nullptr;
        IMFSample *sample = nullptr;
        BYTE *p = nullptr;
        HRESULT hr = MFCreateMemoryBuffer(FRAME_SIZE, &buf);
        if (SUCCEEDED(hr)) hr = buf->Lock(&p, nullptr, nullptr);
        if (SUCCEEDED(hr)) {
            FillFrame(p);
            buf->Unlock();
            hr = buf->SetCurrentLength(FRAME_SIZE);
        }
        if (SUCCEEDED(hr)) hr = MFCreateSample(&sample);
        if (SUCCEEDED(hr)) hr = sample->AddBuffer(buf);
        if (SUCCEEDED(hr)) hr = sample->SetSampleTime(MFGetSystemTime());
        if (SUCCEEDED(hr)) hr = sample->SetSampleDuration(FRAME_DURATION);
        if (SUCCEEDED(hr) && token) hr = sample->SetUnknown(MFSampleExtension_Token, token);
        if (SUCCEEDED(hr)) {
            IMFMediaEventQueue *q = nullptr;
            EnterCriticalSection(&cs);
            if (!isShutdown && state == MF_STREAM_STATE_RUNNING && queue) {
                q = queue;
                q->AddRef();
            }
            LeaveCriticalSection(&cs);
            if (q) {
                hr = q->QueueEventParamUnk(MEMediaSample, GUID_NULL, S_OK, sample);
                q->Release();
            }
        }
        if (FAILED(hr)) Log("stream: deliver failed 0x%08lx", hr);
        SafeRelease(sample);
        SafeRelease(buf);
    }

    IMFMediaEventQueue *GetQueue() { // returns AddRef'd queue or nullptr if shut down
        EnterCriticalSection(&cs);
        IMFMediaEventQueue *q = isShutdown ? nullptr : queue;
        if (q) q->AddRef();
        LeaveCriticalSection(&cs);
        return q;
    }

  public:
    Stream() {
        InitializeCriticalSection(&cs);
        InterlockedIncrement(&g_objects);
    }

    HRESULT Init(IMFMediaSource *src) {
        source = src;
        source->AddRef();
        HRESULT hr = MFCreateAttributes(&attrs, 8);
        if (SUCCEEDED(hr)) hr = SetStreamAttrs(attrs);
        if (SUCCEEDED(hr)) hr = MFCreateEventQueue(&queue);

        IMFMediaType *mt = nullptr;
        if (SUCCEEDED(hr)) hr = MFCreateMediaType(&mt);
        if (SUCCEEDED(hr)) hr = mt->SetGUID(MF_MT_MAJOR_TYPE, MFMediaType_Video);
        if (SUCCEEDED(hr)) hr = mt->SetGUID(MF_MT_SUBTYPE, MFVideoFormat_NV12);
        if (SUCCEEDED(hr)) hr = MFSetAttributeSize(mt, MF_MT_FRAME_SIZE, FRAME_W, FRAME_H);
        if (SUCCEEDED(hr)) hr = MFSetAttributeRatio(mt, MF_MT_FRAME_RATE, 30, 1);
        if (SUCCEEDED(hr)) hr = MFSetAttributeRatio(mt, MF_MT_PIXEL_ASPECT_RATIO, 1, 1);
        if (SUCCEEDED(hr)) hr = mt->SetUINT32(MF_MT_INTERLACE_MODE, MFVideoInterlace_Progressive);
        if (SUCCEEDED(hr)) hr = mt->SetUINT32(MF_MT_ALL_SAMPLES_INDEPENDENT, TRUE);
        if (SUCCEEDED(hr)) hr = mt->SetUINT32(MF_MT_FIXED_SIZE_SAMPLES, TRUE);
        if (SUCCEEDED(hr)) hr = mt->SetUINT32(MF_MT_SAMPLE_SIZE, FRAME_SIZE);
        if (SUCCEEDED(hr)) hr = mt->SetUINT32(MF_MT_DEFAULT_STRIDE, FRAME_W);
        if (SUCCEEDED(hr)) hr = mt->SetUINT32(MF_MT_AVG_BITRATE, FRAME_SIZE * 8 * 30);
        if (SUCCEEDED(hr)) hr = mt->SetUINT32(MF_MT_YUV_MATRIX, MFVideoTransferMatrix_BT601);
        if (SUCCEEDED(hr)) hr = mt->SetUINT32(MF_MT_VIDEO_NOMINAL_RANGE, MFNominalRange_16_235);
        if (SUCCEEDED(hr)) hr = MFCreateStreamDescriptor(0, 1, &mt, &sd);

        IMFMediaTypeHandler *h = nullptr;
        if (SUCCEEDED(hr)) hr = sd->GetMediaTypeHandler(&h);
        if (SUCCEEDED(hr)) hr = h->SetCurrentMediaType(mt);
        if (SUCCEEDED(hr)) hr = SetStreamAttrs(sd);
        SafeRelease(h);
        SafeRelease(mt);

        if (SUCCEEDED(hr)) {
            wake = CreateEventW(nullptr, FALSE, FALSE, nullptr);
            AddRef(); // owned by worker
            HANDLE t = CreateThread(nullptr, 0, WorkerThunk, this, 0, nullptr);
            if (t) CloseHandle(t);
            else {
                Release();
                hr = HRESULT_FROM_WIN32(GetLastError());
            }
        }
        return hr;
    }

    HRESULT Start(const PROPVARIANT *time) {
        EnterCriticalSection(&cs);
        HRESULT hr = isShutdown ? MF_E_SHUTDOWN : S_OK;
        if (SUCCEEDED(hr)) {
            state = MF_STREAM_STATE_RUNNING;
            hr = queue->QueueEventParamVar(MEStreamStarted, GUID_NULL, S_OK, time);
        }
        LeaveCriticalSection(&cs);
        Log("stream: start 0x%08lx", hr);
        return hr;
    }

    HRESULT Stop(const PROPVARIANT *time) {
        EnterCriticalSection(&cs);
        HRESULT hr = isShutdown ? MF_E_SHUTDOWN : S_OK;
        if (SUCCEEDED(hr)) {
            state = MF_STREAM_STATE_STOPPED;
            for (auto t : tokens) if (t) t->Release();
            tokens.clear();
            hr = queue->QueueEventParamVar(MEStreamStopped, GUID_NULL, S_OK, time);
        }
        LeaveCriticalSection(&cs);
        Log("stream: stop");
        return hr;
    }

    HRESULT Shutdown() {
        EnterCriticalSection(&cs);
        if (!isShutdown) {
            isShutdown = true;
            if (queue) queue->Shutdown();
            for (auto t : tokens) if (t) t->Release();
            tokens.clear();
            SafeRelease(source); // break the source <-> stream cycle
        }
        LeaveCriticalSection(&cs);
        InterlockedExchange(&stopWorker, 1);
        if (wake) SetEvent(wake);
        return S_OK;
    }

    HRESULT GetAttributes(IMFAttributes **pp) {
        EnterCriticalSection(&cs);
        HRESULT hr = isShutdown ? MF_E_SHUTDOWN : S_OK;
        if (SUCCEEDED(hr)) {
            *pp = attrs;
            attrs->AddRef();
        }
        LeaveCriticalSection(&cs);
        return hr;
    }

    // IUnknown
    STDMETHODIMP QueryInterface(REFIID riid, void **ppv) override {
        if (!ppv) return E_POINTER;
        if (riid == IID_IUnknown || riid == IID_IMFMediaEventGenerator || riid == IID_IMFMediaStream ||
            riid == IID_IMFMediaStream2_)
            *ppv = static_cast<IMFMediaStream2X *>(this);
        else if (riid == IID_IKsControl_)
            *ppv = static_cast<IKsControlX *>(this);
        else {
            *ppv = nullptr;
            return E_NOINTERFACE;
        }
        AddRef();
        return S_OK;
    }
    STDMETHODIMP_(ULONG) AddRef() override { return InterlockedIncrement(&ref); }
    STDMETHODIMP_(ULONG) Release() override {
        LONG r = InterlockedDecrement(&ref);
        if (r == 0) delete this;
        return r;
    }

    // IMFMediaEventGenerator
    STDMETHODIMP GetEvent(DWORD flags, IMFMediaEvent **ev) override {
        IMFMediaEventQueue *q = GetQueue();
        if (!q) return MF_E_SHUTDOWN;
        HRESULT hr = q->GetEvent(flags, ev);
        q->Release();
        return hr;
    }
    STDMETHODIMP BeginGetEvent(IMFAsyncCallback *cb, IUnknown *st) override {
        IMFMediaEventQueue *q = GetQueue();
        if (!q) return MF_E_SHUTDOWN;
        HRESULT hr = q->BeginGetEvent(cb, st);
        q->Release();
        return hr;
    }
    STDMETHODIMP EndGetEvent(IMFAsyncResult *r, IMFMediaEvent **ev) override {
        IMFMediaEventQueue *q = GetQueue();
        if (!q) return MF_E_SHUTDOWN;
        HRESULT hr = q->EndGetEvent(r, ev);
        q->Release();
        return hr;
    }
    STDMETHODIMP QueueEvent(MediaEventType t, REFGUID ext, HRESULT st, const PROPVARIANT *v) override {
        IMFMediaEventQueue *q = GetQueue();
        if (!q) return MF_E_SHUTDOWN;
        HRESULT hr = q->QueueEventParamVar(t, ext, st, v);
        q->Release();
        return hr;
    }

    // IMFMediaStream
    STDMETHODIMP GetMediaSource(IMFMediaSource **pp) override {
        if (!pp) return E_POINTER;
        EnterCriticalSection(&cs);
        HRESULT hr = (isShutdown || !source) ? MF_E_SHUTDOWN : S_OK;
        if (SUCCEEDED(hr)) {
            *pp = source;
            source->AddRef();
        }
        LeaveCriticalSection(&cs);
        return hr;
    }
    STDMETHODIMP GetStreamDescriptor(IMFStreamDescriptor **pp) override {
        if (!pp) return E_POINTER;
        EnterCriticalSection(&cs);
        HRESULT hr = isShutdown ? MF_E_SHUTDOWN : S_OK;
        if (SUCCEEDED(hr)) {
            *pp = sd;
            sd->AddRef();
        }
        LeaveCriticalSection(&cs);
        return hr;
    }
    STDMETHODIMP RequestSample(IUnknown *token) override {
        EnterCriticalSection(&cs);
        HRESULT hr = S_OK;
        if (isShutdown) hr = MF_E_SHUTDOWN;
        else if (state != MF_STREAM_STATE_RUNNING) hr = MF_E_INVALIDREQUEST;
        else {
            if (token) token->AddRef();
            tokens.push_back(token);
            if (tokens.size() > 8) { // never let the queue grow unbounded
                IUnknown *old = tokens.front();
                tokens.pop_front();
                if (old) old->Release();
            }
        }
        LeaveCriticalSection(&cs);
        if (SUCCEEDED(hr)) SetEvent(wake);
        return hr;
    }

    // IMFMediaStream2
    STDMETHODIMP SetStreamState(MF_STREAM_STATE value) override {
        EnterCriticalSection(&cs);
        HRESULT hr = isShutdown ? MF_E_SHUTDOWN : S_OK;
        if (SUCCEEDED(hr)) {
            if (value == MF_STREAM_STATE_STOPPED || value == MF_STREAM_STATE_PAUSED ||
                value == MF_STREAM_STATE_RUNNING)
                state = value;
            else
                hr = MF_E_INVALID_STATE_TRANSITION;
        }
        LeaveCriticalSection(&cs);
        return hr;
    }
    STDMETHODIMP GetStreamState(MF_STREAM_STATE *value) override {
        if (!value) return E_POINTER;
        EnterCriticalSection(&cs);
        *value = state;
        LeaveCriticalSection(&cs);
        return S_OK;
    }

    // IKsControl
    STDMETHODIMP KsProperty(void *, ULONG, void *, ULONG, ULONG *) override {
        return HRESULT_FROM_WIN32(ERROR_SET_NOT_FOUND);
    }
    STDMETHODIMP KsMethod(void *, ULONG, void *, ULONG, ULONG *) override {
        return HRESULT_FROM_WIN32(ERROR_SET_NOT_FOUND);
    }
    STDMETHODIMP KsEvent(void *, ULONG, void *, ULONG, ULONG *) override {
        return HRESULT_FROM_WIN32(ERROR_SET_NOT_FOUND);
    }
};

// ------------------------------------------------------------------ source

class Source : public IMFMediaSourceEx, public IMFGetService, public IKsControlX {
    LONG ref = 1;
    CRITICAL_SECTION cs;
    IMFMediaEventQueue *queue = nullptr;
    IMFPresentationDescriptor *pd = nullptr;
    IMFAttributes *attrs = nullptr;
    Stream *stream = nullptr;
    bool isShutdown = false;
    bool mfStarted = false;

    ~Source() {
        SafeRelease(queue);
        SafeRelease(pd);
        SafeRelease(attrs);
        SafeRelease(stream);
        if (mfStarted) MFShutdown();
        DeleteCriticalSection(&cs);
        InterlockedDecrement(&g_objects);
        Log("source: destroyed");
    }

    IMFMediaEventQueue *GetQueue() {
        EnterCriticalSection(&cs);
        IMFMediaEventQueue *q = isShutdown ? nullptr : queue;
        if (q) q->AddRef();
        LeaveCriticalSection(&cs);
        return q;
    }

  public:
    Source() {
        InitializeCriticalSection(&cs);
        InterlockedIncrement(&g_objects);
    }

    HRESULT Init(IMFAttributes *from) {
        HRESULT hr = MFStartup(MF_VERSION, MFSTARTUP_LITE);
        mfStarted = SUCCEEDED(hr);
        hr = MFCreateAttributes(&attrs, 8);
        if (SUCCEEDED(hr) && from) hr = from->CopyAllItems(attrs);
        if (SUCCEEDED(hr)) hr = MFCreateEventQueue(&queue);
        if (SUCCEEDED(hr)) {
            stream = new Stream();
            hr = stream->Init(this);
        }
        IMFStreamDescriptor *sd = nullptr;
        if (SUCCEEDED(hr)) hr = stream->GetStreamDescriptor(&sd);
        if (SUCCEEDED(hr)) hr = MFCreatePresentationDescriptor(1, &sd, &pd);
        if (SUCCEEDED(hr)) hr = pd->SelectStream(0);
        SafeRelease(sd);
        if (SUCCEEDED(hr)) EnsureFeed();
        Log("source: init 0x%08lx", hr);
        return hr;
    }

    // IUnknown
    STDMETHODIMP QueryInterface(REFIID riid, void **ppv) override {
        if (!ppv) return E_POINTER;
        if (riid == IID_IUnknown || riid == IID_IMFMediaEventGenerator || riid == IID_IMFMediaSource ||
            riid == IID_IMFMediaSourceEx)
            *ppv = static_cast<IMFMediaSourceEx *>(this);
        else if (riid == IID_IMFGetService)
            *ppv = static_cast<IMFGetService *>(this);
        else if (riid == IID_IKsControl_)
            *ppv = static_cast<IKsControlX *>(this);
        else {
            *ppv = nullptr;
            return E_NOINTERFACE;
        }
        AddRef();
        return S_OK;
    }
    STDMETHODIMP_(ULONG) AddRef() override { return InterlockedIncrement(&ref); }
    STDMETHODIMP_(ULONG) Release() override {
        LONG r = InterlockedDecrement(&ref);
        if (r == 0) delete this;
        return r;
    }

    // IMFMediaEventGenerator
    STDMETHODIMP GetEvent(DWORD flags, IMFMediaEvent **ev) override {
        IMFMediaEventQueue *q = GetQueue();
        if (!q) return MF_E_SHUTDOWN;
        HRESULT hr = q->GetEvent(flags, ev);
        q->Release();
        return hr;
    }
    STDMETHODIMP BeginGetEvent(IMFAsyncCallback *cb, IUnknown *st) override {
        IMFMediaEventQueue *q = GetQueue();
        if (!q) return MF_E_SHUTDOWN;
        HRESULT hr = q->BeginGetEvent(cb, st);
        q->Release();
        return hr;
    }
    STDMETHODIMP EndGetEvent(IMFAsyncResult *r, IMFMediaEvent **ev) override {
        IMFMediaEventQueue *q = GetQueue();
        if (!q) return MF_E_SHUTDOWN;
        HRESULT hr = q->EndGetEvent(r, ev);
        q->Release();
        return hr;
    }
    STDMETHODIMP QueueEvent(MediaEventType t, REFGUID ext, HRESULT st, const PROPVARIANT *v) override {
        IMFMediaEventQueue *q = GetQueue();
        if (!q) return MF_E_SHUTDOWN;
        HRESULT hr = q->QueueEventParamVar(t, ext, st, v);
        q->Release();
        return hr;
    }

    // IMFMediaSource
    STDMETHODIMP GetCharacteristics(DWORD *c) override {
        if (!c) return E_POINTER;
        EnterCriticalSection(&cs);
        HRESULT hr = isShutdown ? MF_E_SHUTDOWN : S_OK;
        *c = MFMEDIASOURCE_IS_LIVE;
        LeaveCriticalSection(&cs);
        return hr;
    }
    STDMETHODIMP CreatePresentationDescriptor(IMFPresentationDescriptor **pp) override {
        if (!pp) return E_POINTER;
        EnterCriticalSection(&cs);
        HRESULT hr = isShutdown ? MF_E_SHUTDOWN : pd->Clone(pp);
        LeaveCriticalSection(&cs);
        return hr;
    }
    STDMETHODIMP Start(IMFPresentationDescriptor *desc, const GUID *timeFormat, const PROPVARIANT *) override {
        if (!desc) return E_INVALIDARG;
        if (timeFormat && *timeFormat != GUID_NULL) return MF_E_UNSUPPORTED_TIME_FORMAT;
        EnterCriticalSection(&cs);
        HRESULT hr = isShutdown ? MF_E_SHUTDOWN : S_OK;
        PROPVARIANT time;
        PropVariantInit(&time);
        time.vt = VT_I8;
        time.hVal.QuadPart = MFGetSystemTime();
        DWORD count = 0;
        if (SUCCEEDED(hr)) hr = desc->GetStreamDescriptorCount(&count);
        for (DWORD i = 0; SUCCEEDED(hr) && i < count; i++) {
            BOOL selected = FALSE;
            IMFStreamDescriptor *sd = nullptr;
            hr = desc->GetStreamDescriptorByIndex(i, &selected, &sd);
            DWORD id = 0;
            if (SUCCEEDED(hr)) hr = sd->GetStreamIdentifier(&id);
            SafeRelease(sd);
            if (FAILED(hr)) break;
            if (id != 0) {
                hr = E_INVALIDARG;
                break;
            }
            MF_STREAM_STATE st;
            stream->GetStreamState(&st);
            bool running = st != MF_STREAM_STATE_STOPPED;
            if (selected && !running) {
                pd->SelectStream(0);
                hr = queue->QueueEventParamUnk(MENewStream, GUID_NULL, S_OK,
                                               static_cast<IMFMediaStream2X *>(stream));
                if (SUCCEEDED(hr)) hr = stream->Start(&time);
            } else if (!selected && running) {
                pd->DeselectStream(0);
                hr = stream->Stop(&time);
            }
        }
        if (SUCCEEDED(hr) || hr != MF_E_SHUTDOWN) {
            HRESULT qhr = isShutdown ? MF_E_SHUTDOWN : queue->QueueEventParamVar(MESourceStarted, GUID_NULL, hr, &time);
            if (SUCCEEDED(hr)) hr = qhr;
        }
        LeaveCriticalSection(&cs);
        Log("source: start 0x%08lx", hr);
        return hr;
    }
    STDMETHODIMP Stop() override {
        EnterCriticalSection(&cs);
        HRESULT hr = isShutdown ? MF_E_SHUTDOWN : S_OK;
        if (SUCCEEDED(hr)) {
            PROPVARIANT time;
            PropVariantInit(&time);
            time.vt = VT_I8;
            time.hVal.QuadPart = MFGetSystemTime();
            MF_STREAM_STATE st;
            stream->GetStreamState(&st);
            if (st != MF_STREAM_STATE_STOPPED) stream->Stop(&time);
            pd->DeselectStream(0);
            hr = queue->QueueEventParamVar(MESourceStopped, GUID_NULL, S_OK, &time);
        }
        LeaveCriticalSection(&cs);
        Log("source: stop");
        return hr;
    }
    STDMETHODIMP Pause() override { return MF_E_INVALID_STATE_TRANSITION; }
    STDMETHODIMP Shutdown() override {
        EnterCriticalSection(&cs);
        if (!isShutdown) {
            isShutdown = true;
            if (queue) queue->Shutdown();
            if (stream) stream->Shutdown();
        }
        LeaveCriticalSection(&cs);
        Log("source: shutdown");
        return S_OK;
    }

    // IMFMediaSourceEx
    STDMETHODIMP GetSourceAttributes(IMFAttributes **pp) override {
        if (!pp) return E_POINTER;
        EnterCriticalSection(&cs);
        HRESULT hr = isShutdown ? MF_E_SHUTDOWN : S_OK;
        if (SUCCEEDED(hr)) {
            *pp = attrs;
            attrs->AddRef();
        }
        LeaveCriticalSection(&cs);
        return hr;
    }
    STDMETHODIMP GetStreamAttributes(DWORD id, IMFAttributes **pp) override {
        if (!pp) return E_POINTER;
        if (id != 0) return MF_E_INVALIDSTREAMNUMBER;
        EnterCriticalSection(&cs);
        HRESULT hr = isShutdown ? MF_E_SHUTDOWN : stream->GetAttributes(pp);
        LeaveCriticalSection(&cs);
        return hr;
    }
    STDMETHODIMP SetD3DManager(IUnknown *) override { return S_OK; } // CPU memory samples only

    // IMFGetService
    STDMETHODIMP GetService(REFGUID, REFIID, LPVOID *ppv) override {
        if (ppv) *ppv = nullptr;
        return MF_E_UNSUPPORTED_SERVICE;
    }

    // IKsControl
    STDMETHODIMP KsProperty(void *, ULONG, void *, ULONG, ULONG *) override {
        return HRESULT_FROM_WIN32(ERROR_SET_NOT_FOUND);
    }
    STDMETHODIMP KsMethod(void *, ULONG, void *, ULONG, ULONG *) override {
        return HRESULT_FROM_WIN32(ERROR_SET_NOT_FOUND);
    }
    STDMETHODIMP KsEvent(void *, ULONG, void *, ULONG, ULONG *) override {
        return HRESULT_FROM_WIN32(ERROR_SET_NOT_FOUND);
    }
};

// --------------------------------------------------------------- activator

#define FWD(name, params, args) \
    STDMETHODIMP name params override { return attrs->name args; }

class Activator : public IMFActivate {
    LONG ref = 1;
    IMFAttributes *attrs = nullptr;
    ~Activator() {
        SafeRelease(attrs);
        InterlockedDecrement(&g_objects);
    }

  public:
    Activator() { InterlockedIncrement(&g_objects); }
    HRESULT Init() { return MFCreateAttributes(&attrs, 4); }

    STDMETHODIMP QueryInterface(REFIID riid, void **ppv) override {
        if (!ppv) return E_POINTER;
        if (riid == IID_IUnknown || riid == IID_IMFAttributes || riid == IID_IMFActivate)
            *ppv = static_cast<IMFActivate *>(this);
        else {
            *ppv = nullptr;
            return E_NOINTERFACE;
        }
        AddRef();
        return S_OK;
    }
    STDMETHODIMP_(ULONG) AddRef() override { return InterlockedIncrement(&ref); }
    STDMETHODIMP_(ULONG) Release() override {
        LONG r = InterlockedDecrement(&ref);
        if (r == 0) delete this;
        return r;
    }

    // IMFAttributes -> inner store
    FWD(GetItem, (REFGUID k, PROPVARIANT *v), (k, v))
    FWD(GetItemType, (REFGUID k, MF_ATTRIBUTE_TYPE *t), (k, t))
    FWD(CompareItem, (REFGUID k, REFPROPVARIANT v, WINBOOL *r), (k, v, r))
    FWD(Compare, (IMFAttributes *o, MF_ATTRIBUTES_MATCH_TYPE m, WINBOOL *r), (o, m, r))
    FWD(GetUINT32, (REFGUID k, UINT32 *v), (k, v))
    FWD(GetUINT64, (REFGUID k, UINT64 *v), (k, v))
    FWD(GetDouble, (REFGUID k, double *v), (k, v))
    FWD(GetGUID, (REFGUID k, GUID *v), (k, v))
    FWD(GetStringLength, (REFGUID k, UINT32 *v), (k, v))
    FWD(GetString, (REFGUID k, LPWSTR s, UINT32 c, UINT32 *l), (k, s, c, l))
    FWD(GetAllocatedString, (REFGUID k, LPWSTR *s, UINT32 *l), (k, s, l))
    FWD(GetBlobSize, (REFGUID k, UINT32 *v), (k, v))
    FWD(GetBlob, (REFGUID k, UINT8 *b, UINT32 c, UINT32 *l), (k, b, c, l))
    FWD(GetAllocatedBlob, (REFGUID k, UINT8 **b, UINT32 *l), (k, b, l))
    FWD(GetUnknown, (REFGUID k, REFIID i, LPVOID *p), (k, i, p))
    FWD(SetItem, (REFGUID k, REFPROPVARIANT v), (k, v))
    FWD(DeleteItem, (REFGUID k), (k))
    FWD(DeleteAllItems, (), ())
    FWD(SetUINT32, (REFGUID k, UINT32 v), (k, v))
    FWD(SetUINT64, (REFGUID k, UINT64 v), (k, v))
    FWD(SetDouble, (REFGUID k, double v), (k, v))
    FWD(SetGUID, (REFGUID k, REFGUID v), (k, v))
    FWD(SetString, (REFGUID k, LPCWSTR v), (k, v))
    FWD(SetBlob, (REFGUID k, const UINT8 *b, UINT32 l), (k, b, l))
    FWD(SetUnknown, (REFGUID k, IUnknown *u), (k, u))
    FWD(LockStore, (), ())
    FWD(UnlockStore, (), ())
    FWD(GetCount, (UINT32 * c), (c))
    FWD(GetItemByIndex, (UINT32 i, GUID *k, PROPVARIANT *v), (i, k, v))
    FWD(CopyAllItems, (IMFAttributes * d), (d))

    // IMFActivate
    STDMETHODIMP ActivateObject(REFIID riid, void **ppv) override {
        if (!ppv) return E_POINTER;
        *ppv = nullptr;
        Source *s = new Source();
        HRESULT hr = s->Init(attrs);
        if (SUCCEEDED(hr)) hr = s->QueryInterface(riid, ppv);
        s->Release();
        Log("activator: ActivateObject 0x%08lx", hr);
        return hr;
    }
    STDMETHODIMP ShutdownObject() override { return S_OK; }
    STDMETHODIMP DetachObject() override { return S_OK; }
};

// ------------------------------------------------------------ class factory

class Factory : public IClassFactory {
  public:
    STDMETHODIMP QueryInterface(REFIID riid, void **ppv) override {
        if (!ppv) return E_POINTER;
        if (riid == IID_IUnknown || riid == IID_IClassFactory) {
            *ppv = static_cast<IClassFactory *>(this);
            return S_OK;
        }
        *ppv = nullptr;
        return E_NOINTERFACE;
    }
    STDMETHODIMP_(ULONG) AddRef() override { return 2; }
    STDMETHODIMP_(ULONG) Release() override { return 1; }
    STDMETHODIMP CreateInstance(IUnknown *outer, REFIID riid, void **ppv) override {
        if (!ppv) return E_POINTER;
        *ppv = nullptr;
        if (outer) return CLASS_E_NOAGGREGATION;
        HRESULT hr;
        if (riid == IID_IMFMediaSource || riid == IID_IMFMediaSourceEx) {
            Source *s = new Source();
            hr = s->Init(nullptr);
            if (SUCCEEDED(hr)) hr = s->QueryInterface(riid, ppv);
            s->Release();
        } else {
            Activator *a = new Activator();
            hr = a->Init();
            if (SUCCEEDED(hr)) hr = a->QueryInterface(riid, ppv);
            a->Release();
        }
        Log("factory: CreateInstance 0x%08lx", hr);
        return hr;
    }
    STDMETHODIMP LockServer(BOOL) override { return S_OK; }
};

static Factory g_factory;

// ------------------------------------------------------------- DLL exports

extern "C" BOOL WINAPI DllMain(HINSTANCE h, DWORD reason, LPVOID) {
    if (reason == DLL_PROCESS_ATTACH) {
        g_module = h;
        DisableThreadLibraryCalls(h);
    }
    return TRUE;
}

extern "C" HRESULT WINAPI PC_DllGetClassObject(REFCLSID clsid, REFIID riid, void **ppv) {
    if (!ppv) return E_POINTER;
    *ppv = nullptr;
    if (clsid != CLSID_PhoneCam) return CLASS_E_CLASSNOTAVAILABLE;
    return g_factory.QueryInterface(riid, ppv);
}

// The feed thread and MF objects may still be alive; keep the DLL loaded.
extern "C" HRESULT WINAPI PC_DllCanUnloadNow() { return S_FALSE; }

static LSTATUS SetStr(HKEY key, const wchar_t *name, const wchar_t *value) {
    return RegSetValueExW(key, name, 0, REG_SZ, (const BYTE *)value, (DWORD)((wcslen(value) + 1) * sizeof(wchar_t)));
}

extern "C" HRESULT WINAPI PC_DllRegisterServer() {
    wchar_t path[MAX_PATH];
    if (!GetModuleFileNameW(g_module, path, MAX_PATH)) return HRESULT_FROM_WIN32(GetLastError());
    wchar_t keyPath[256];
    swprintf(keyPath, 256, L"SOFTWARE\\Classes\\CLSID\\%ls", CLSID_PhoneCam_Str);
    HKEY key, inproc;
    LSTATUS st = RegCreateKeyExW(HKEY_LOCAL_MACHINE, keyPath, 0, nullptr, 0, KEY_WRITE, nullptr, &key, nullptr);
    if (st != ERROR_SUCCESS) return HRESULT_FROM_WIN32(st);
    SetStr(key, nullptr, L"PhoneCam Virtual Camera Source");
    st = RegCreateKeyExW(key, L"InprocServer32", 0, nullptr, 0, KEY_WRITE, nullptr, &inproc, nullptr);
    if (st == ERROR_SUCCESS) {
        SetStr(inproc, nullptr, path);
        SetStr(inproc, L"ThreadingModel", L"Both");
        RegCloseKey(inproc);
    }
    RegCloseKey(key);
    return HRESULT_FROM_WIN32(st);
}

extern "C" HRESULT WINAPI PC_DllUnregisterServer() {
    wchar_t keyPath[256];
    swprintf(keyPath, 256, L"SOFTWARE\\Classes\\CLSID\\%ls", CLSID_PhoneCam_Str);
    LSTATUS st = RegDeleteTreeW(HKEY_LOCAL_MACHINE, keyPath);
    return (st == ERROR_SUCCESS || st == ERROR_FILE_NOT_FOUND) ? S_OK : HRESULT_FROM_WIN32(st);
}
