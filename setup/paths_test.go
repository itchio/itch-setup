package setup

import (
	"testing"
)

func Test_PathWithinDir(t *testing.T) {
	cases := []struct {
		path string
		dir  string
		want bool
	}{
		// basic containment, windows separators
		{`C:\Users\leafo\AppData\Local\kitch\app-26.10.0\kitch.exe`, `C:\Users\leafo\AppData\Local\kitch\app-26.10.0`, true},
		// case-insensitive
		{`c:\users\LEAFO\appdata\local\kitch\app-26.10.0\Kitch.exe`, `C:\Users\leafo\AppData\Local\kitch\app-26.10.0`, true},
		// component boundary: app-26.1 must not swallow app-26.10.0
		{`C:\kitch\app-26.10.0\kitch.exe`, `C:\kitch\app-26.1`, false},
		// dir itself is not "within"
		{`C:\kitch\app-26.10.0`, `C:\kitch\app-26.10.0`, false},
		// nested deeper
		{`C:\kitch\app-26.10.0\resources\app\kitch.exe`, `C:\kitch\app-26.10.0`, true},
		// unrelated
		{`C:\Games\kitch.exe`, `C:\kitch\app-26.10.0`, false},
		// trailing separator on dir
		{`C:\kitch\app-26.10.0\kitch.exe`, `C:\kitch\app-26.10.0\`, true},
		// forward slashes
		{`/home/leafo/.itch/app-26.10.0/itch`, `/home/leafo/.itch/app-26.10.0`, true},
		// empties
		{``, `C:\kitch`, false},
		{`C:\kitch\a.exe`, ``, false},
	}

	for _, c := range cases {
		got := PathWithinDir(c.path, c.dir)
		if got != c.want {
			t.Errorf("PathWithinDir(%q, %q) = %v, want %v", c.path, c.dir, got, c.want)
		}
	}
}

func Test_IsStaleAppShortcutTarget(t *testing.T) {
	installRoot := `C:\Users\leafo\AppData\Local\kitch`

	cases := []struct {
		name    string
		target  string
		root    string
		exeName string
		want    bool
	}{
		{
			"the observed stale shortcut",
			`C:\Users\leafo\AppData\Local\kitch\app-26.10.0\kitch.exe`,
			installRoot, "kitch.exe", true,
		},
		{
			"case differences",
			`c:\users\leafo\appdata\local\KITCH\App-26.10.0\Kitch.EXE`,
			installRoot, "kitch.exe", true,
		},
		{
			"points at the launcher (already migrated)",
			`C:\Users\leafo\AppData\Local\kitch\itch-setup.exe`,
			installRoot, "kitch.exe", false,
		},
		{
			"same exe name in another directory",
			`C:\Games\app-26.10.0\kitch.exe`,
			installRoot, "kitch.exe", false,
		},
		{
			"different exe inside a version dir",
			`C:\Users\leafo\AppData\Local\kitch\app-26.10.0\uninstall.exe`,
			installRoot, "kitch.exe", false,
		},
		{
			"nested too deep",
			`C:\Users\leafo\AppData\Local\kitch\app-26.10.0\resources\kitch.exe`,
			installRoot, "kitch.exe", false,
		},
		{
			"non-versioned subdirectory",
			`C:\Users\leafo\AppData\Local\kitch\staging\kitch.exe`,
			installRoot, "kitch.exe", false,
		},
	}

	for _, c := range cases {
		got := IsStaleAppShortcutTarget(c.target, c.root, c.exeName)
		if got != c.want {
			t.Errorf("%s: IsStaleAppShortcutTarget(%q, %q, %q) = %v, want %v",
				c.name, c.target, c.root, c.exeName, got, c.want)
		}
	}
}
