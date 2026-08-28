package rungame

import (
	"strings"
	"testing"
)

func TestScanButlerOutput(t *testing.T) {
	needsApp, profileGone := scanButlerOutput(strings.NewReader(
		`{"type":"log","message":"hi"}` + "\n" +
			`{"type":"launch/needs-app","reason":"html game","uploadId":123}` + "\n"))
	if needsApp == nil {
		t.Fatal("expected needs-app payload")
	}
	if needsApp.reason != "html game" || needsApp.uploadID != 123 {
		t.Errorf("got %+v", needsApp)
	}
	if profileGone {
		t.Error("unexpected profileGone")
	}

	needsApp, profileGone = scanButlerOutput(strings.NewReader(
		`{"type":"launch/profile-not-found","profileId":7}` + "\n"))
	if needsApp != nil {
		t.Error("unexpected needs-app payload")
	}
	if !profileGone {
		t.Error("expected profileGone")
	}
}

func TestScanButlerOutputLongLine(t *testing.T) {
	// an over-buffer line must not stop the reader early: the rest of
	// the stream still has to be consumed so butler never blocks on a
	// full pipe
	long := strings.Repeat("x", 2*1024*1024)
	r := strings.NewReader(long + "\n" + `{"type":"log","message":"after"}` + "\n")
	scanButlerOutput(r)
	if r.Len() != 0 {
		t.Errorf("reader not drained, %d bytes left", r.Len())
	}
}

func TestWithoutOverlayEntries(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/lib/gameoverlayrenderer.so", ""},
		{"/lib/gameoverlayrenderer.so:/usr/lib/mangohud.so", "/usr/lib/mangohud.so"},
		{"/usr/lib/mangohud.so /lib64/gameoverlayrenderer.so", "/usr/lib/mangohud.so"},
		{"/usr/lib/mangohud.so", "/usr/lib/mangohud.so"},
		{"", ""},
	}
	for _, c := range cases {
		if got := withoutOverlayEntries(c.in); got != c.want {
			t.Errorf("withoutOverlayEntries(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
