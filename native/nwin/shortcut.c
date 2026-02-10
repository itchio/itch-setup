#define COBJMACROS
#define CINTERFACE
#define WIN32_LEAN_AND_MEAN

#include "shortcut.h"
#include <objbase.h>
#include <shlobj.h>
#include <propvarutil.h>
#include <propkey.h>

// IPropertyStore GUID — not always in MinGW headers
static const GUID IID_IPropertyStore_local =
    {0x886d8eeb, 0x8cf2, 0x4446, {0x8d, 0x02, 0xcd, 0xba, 0x1d, 0xbd, 0xcf, 0x99}};

// PKEY_AppUserModel_ID — {9F4C2855-9F79-4B39-A8D0-E1D42DE1D5F3}, 5
static const PROPERTYKEY PKEY_AppUserModel_ID_local =
    {{0x9F4C2855, 0x9F79, 0x4B39, {0xA8, 0xD0, 0xE1, 0xD4, 0x2D, 0xE1, 0xD5, 0xF3}}, 5};

// CreateShortcutWithAppId creates a .lnk shortcut file with an optional AppUserModelId.
// All string parameters are UTF-16 (wchar_t*). Pass NULL for optional fields.
// Returns HRESULT (S_OK = 0 on success).
HRESULT CreateShortcutWithAppId(
    const wchar_t *shortcutPath,
    const wchar_t *targetPath,
    const wchar_t *arguments,
    const wchar_t *description,
    const wchar_t *iconLocation,
    const wchar_t *workingDirectory,
    const wchar_t *appUserModelId
) {
    HRESULT hr;

    hr = CoInitializeEx(NULL, COINIT_APARTMENTTHREADED);
    if (FAILED(hr) && hr != RPC_E_CHANGED_MODE) {
        return hr;
    }

    IShellLinkW *psl = NULL;
    hr = CoCreateInstance(
        &CLSID_ShellLink, NULL, CLSCTX_INPROC_SERVER,
        &IID_IShellLinkW, (void **)&psl
    );
    if (FAILED(hr)) {
        goto cleanup_com;
    }

    if (targetPath) {
        hr = IShellLinkW_SetPath(psl, targetPath);
        if (FAILED(hr)) goto cleanup_sl;
    }

    if (arguments) {
        hr = IShellLinkW_SetArguments(psl, arguments);
        if (FAILED(hr)) goto cleanup_sl;
    }

    if (description) {
        hr = IShellLinkW_SetDescription(psl, description);
        if (FAILED(hr)) goto cleanup_sl;
    }

    if (iconLocation) {
        hr = IShellLinkW_SetIconLocation(psl, iconLocation, 0);
        if (FAILED(hr)) goto cleanup_sl;
    }

    if (workingDirectory) {
        hr = IShellLinkW_SetWorkingDirectory(psl, workingDirectory);
        if (FAILED(hr)) goto cleanup_sl;
    }

    // Set AppUserModelId via IPropertyStore if provided
    if (appUserModelId) {
        IPropertyStore *pps = NULL;
        hr = IShellLinkW_QueryInterface(psl, &IID_IPropertyStore_local, (void **)&pps);
        if (SUCCEEDED(hr)) {
            PROPVARIANT pv;
            hr = InitPropVariantFromString(appUserModelId, &pv);
            if (SUCCEEDED(hr)) {
                hr = IPropertyStore_SetValue(pps, &PKEY_AppUserModel_ID_local, &pv);
                PropVariantClear(&pv);
                if (SUCCEEDED(hr)) {
                    IPropertyStore_Commit(pps);
                }
            }
            IPropertyStore_Release(pps);
            if (FAILED(hr)) goto cleanup_sl;
        } else {
            // IPropertyStore not available — continue without AUMID
            hr = S_OK;
        }
    }

    // Save via IPersistFile
    {
        IPersistFile *ppf = NULL;
        hr = IShellLinkW_QueryInterface(psl, &IID_IPersistFile, (void **)&ppf);
        if (FAILED(hr)) goto cleanup_sl;

        hr = IPersistFile_Save(ppf, shortcutPath, TRUE);
        IPersistFile_Release(ppf);
    }

cleanup_sl:
    IShellLinkW_Release(psl);
cleanup_com:
    CoUninitialize();
    return hr;
}
