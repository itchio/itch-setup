package nwin

import (
	"fmt"
	"log"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// UserProfile is the account that per-user integration (folders,
// shortcuts, HKCU keys) belongs to. Normally that's whoever we run as.
// When we run elevated on the desktop user's behalf it's still them:
// a standard user entering an administrator's credentials at the UAC
// prompt gets a child running under that administrator's profile.
type UserProfile struct {
	token windows.Token // 0: the current process
	sid   string
}

// CurrentUserProfile uses the profile of the current process.
func CurrentUserProfile() *UserProfile {
	return &UserProfile{}
}

// DesktopUserProfile uses the profile of the interactive user of our
// session, for when we run elevated.
func DesktopUserProfile() (*UserProfile, error) {
	tok, err := desktopUserToken()
	if err != nil {
		return nil, err
	}

	tu, err := tok.GetTokenUser()
	if err != nil {
		tok.Close()
		return nil, fmt.Errorf("GetTokenUser: %w", err)
	}

	return &UserProfile{token: tok, sid: tu.User.Sid.String()}, nil
}

func (u *UserProfile) Close() {
	if u.token != 0 {
		u.token.Close()
		u.token = 0
	}
}

func (u *UserProfile) String() string {
	if u.token == 0 {
		return "current user"
	}
	return fmt.Sprintf("desktop user %s", u.sid)
}

// Folders returns the profile's well-known folders, creating any that
// are missing.
func (u *UserProfile) Folders() (Folders, error) {
	if u.token == 0 {
		return GetFolders()
	}

	var f Folders
	var err error
	get := func(id *windows.KNOWNFOLDERID) string {
		if err != nil {
			return ""
		}
		var path string
		path, err = u.token.KnownFolderPath(id, windows.KF_FLAG_CREATE)
		return path
	}
	f.LocalAppData = get(windows.FOLDERID_LocalAppData)
	f.RoamingAppData = get(windows.FOLDERID_RoamingAppData)
	f.Desktop = get(windows.FOLDERID_Desktop)
	f.Programs = get(windows.FOLDERID_Programs)
	if err != nil {
		return Folders{}, fmt.Errorf("known folder of %s: %w", u, err)
	}
	return f, nil
}

// hive returns the profile's registry hive (what HKEY_CURRENT_USER is
// for a process of theirs). The key must be closed after use.
func (u *UserProfile) hive() (registry.Key, error) {
	return u.openRoot("")
}

// classesHive returns the hive behind HKEY_CURRENT_USER\Software\Classes.
// It's a separate hive (UsrClass.dat) mounted under HKEY_USERS as
// <SID>_Classes; writing under <SID>\Software\Classes wouldn't reach
// the shell. The key must be closed after use.
func (u *UserProfile) classesHive() (registry.Key, error) {
	if u.token == 0 {
		return registry.OpenKey(registry.CURRENT_USER, `Software\Classes`, registry.CREATE_SUB_KEY)
	}
	return u.openRoot("_Classes")
}

func (u *UserProfile) openRoot(suffix string) (registry.Key, error) {
	if u.token == 0 {
		// closing a predefined key is a no-op, so callers can treat
		// both cases the same
		return registry.CURRENT_USER, nil
	}

	// loaded as long as the user is logged on, and they are: this is
	// the owner of an explorer.exe in our session
	name := u.sid + suffix
	k, err := registry.OpenKey(registry.USERS, name, registry.CREATE_SUB_KEY|registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err != nil {
		return 0, fmt.Errorf("opening HKEY_USERS\\%s: %w", name, err)
	}
	log.Printf("Using registry hive HKEY_USERS\\%s", name)
	return k, nil
}
