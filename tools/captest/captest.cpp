// Opens the "PhoneCam" camera like a normal app (MF source reader) and
// prints the average Y/U/V of a few frames.
#define WINVER 0x0A00
#define _WIN32_WINNT 0x0A00
#include <windows.h>
#include <initguid.h>
#include <mfapi.h>
#include <mfidl.h>
#include <mfreadwrite.h>
#include <cstdio>
#include <cwchar>

int main() {
    CoInitializeEx(nullptr, COINIT_MULTITHREADED);
    MFStartup(MF_VERSION, 0);
    IMFAttributes *a;
    MFCreateAttributes(&a, 1);
    a->SetGUID(MF_DEVSOURCE_ATTRIBUTE_SOURCE_TYPE, MF_DEVSOURCE_ATTRIBUTE_SOURCE_TYPE_VIDCAP_GUID);
    IMFActivate **devs;
    UINT32 n = 0;
    HRESULT hr = MFEnumDeviceSources(a, &devs, &n);
    printf("enum hr=0x%08lx count=%u\n", hr, n);
    IMFActivate *pick = nullptr;
    for (UINT32 i = 0; i < n; i++) {
        WCHAR *name;
        UINT32 len;
        devs[i]->GetAllocatedString(MF_DEVSOURCE_ATTRIBUTE_FRIENDLY_NAME, &name, &len);
        wprintf(L"  device %u: %ls\n", i, name);
        if (wcsstr(name, L"PhoneCam")) pick = devs[i];
        CoTaskMemFree(name);
    }
    if (!pick) { printf("PhoneCam not found\n"); return 1; }
    IMFMediaSource *src;
    hr = pick->ActivateObject(IID_IMFMediaSource, (void **)&src);
    printf("activate hr=0x%08lx\n", hr);
    if (FAILED(hr)) return 1;
    IMFSourceReader *rd;
    hr = MFCreateSourceReaderFromMediaSource(src, nullptr, &rd);
    printf("reader hr=0x%08lx\n", hr);
    if (FAILED(hr)) return 1;
    IMFMediaType *mt;
    rd->GetCurrentMediaType(MF_SOURCE_READER_FIRST_VIDEO_STREAM, &mt);
    UINT64 sz = 0; GUID sub = {};
    mt->GetUINT64(MF_MT_FRAME_SIZE, &sz);
    mt->GetGUID(MF_MT_SUBTYPE, &sub);
    printf("type %ux%u subtype=%.4s\n", (UINT32)(sz >> 32), (UINT32)sz, (char *)&sub.Data1);
    for (int f = 0; f < 15; f++) {
        DWORD idx, flags; LONGLONG ts; IMFSample *s = nullptr;
        hr = rd->ReadSample(MF_SOURCE_READER_FIRST_VIDEO_STREAM, 0, &idx, &flags, &ts, &s);
        if (FAILED(hr)) { printf("read hr=0x%08lx\n", hr); return 1; }
        if (!s) continue;
        IMFMediaBuffer *b; s->ConvertToContiguousBuffer(&b);
        BYTE *p; DWORD len; b->Lock(&p, nullptr, &len);
        const int W = 1280, H = 720;
        unsigned long long ys = 0, us = 0, vs = 0;
        for (int i = 0; i < W * H; i += 97) ys += p[i];
        int cnt = 0;
        for (int i = W * H; i + 1 < (int)len; i += 2 * 53) { us += p[i]; vs += p[i + 1]; cnt++; }
        printf("frame %2d ts=%lld len=%lu avgY=%llu avgU=%llu avgV=%llu centerY=%u\n", f, ts / 10000, len,
               ys / ((W * H + 96) / 97), us / cnt, vs / cnt, p[(H / 2) * W + W / 2]);
        b->Unlock(); b->Release(); s->Release();
    }
    src->Shutdown();
    return 0;
}
