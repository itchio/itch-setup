package setup

import (
	"strings"
)

// normalizePath lowercases a path and normalizes both kinds of separators
// to "/", so Windows paths can be compared textually. This is only meant
// for comparing paths within a single install root, where casing and
// separator style may differ between sources (registry, shortcuts,
// process enumeration) but refer to the same files.
func normalizePath(p string) string {
	p = strings.ToLower(p)
	p = strings.ReplaceAll(p, "\\", "/")
	for strings.HasSuffix(p, "/") && len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

// PathWithinDir returns true if path refers to a file or directory
// strictly inside dir. The comparison is case-insensitive and respects
// path component boundaries: "C:\\x\\app-26.10.0\\a.exe" is within
// "C:\\x\\app-26.10.0" but not within "C:\\x\\app-26.1".
func PathWithinDir(path string, dir string) bool {
	path = normalizePath(path)
	dir = normalizePath(dir)
	if path == "" || dir == "" {
		return false
	}
	return strings.HasPrefix(path, dir+"/")
}

// IsStaleAppShortcutTarget returns true if target points directly at a
// versioned app executable inside installRoot, i.e. matches
// <installRoot>/app-<version>/<exeName>. Such targets bypass the stable
// itch-setup launcher and go stale as soon as the app self-updates.
func IsStaleAppShortcutTarget(target string, installRoot string, exeName string) bool {
	target = normalizePath(target)
	installRoot = normalizePath(installRoot)
	exeName = strings.ToLower(exeName)
	if target == "" || installRoot == "" || exeName == "" {
		return false
	}

	rel, found := strings.CutPrefix(target, installRoot+"/")
	if !found {
		return false
	}

	parts := strings.Split(rel, "/")
	if len(parts) != 2 {
		return false
	}
	return strings.HasPrefix(parts[0], "app-") && parts[1] == exeName
}
