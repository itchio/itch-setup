#ifndef ITCH_SETUP_SHORTCUT_H
#define ITCH_SETUP_SHORTCUT_H

#include <windows.h>

HRESULT CreateShortcutWithAppId(
    const wchar_t *shortcutPath,
    const wchar_t *targetPath,
    const wchar_t *arguments,
    const wchar_t *description,
    const wchar_t *iconLocation,
    const wchar_t *workingDirectory,
    const wchar_t *appUserModelId
);

#endif
