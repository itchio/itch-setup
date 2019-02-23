package nlinux

// NativeUI encompasses all the user interface itch-setup can show
type NativeUI interface {
	// Initialize
	Init()
	// Create and show the main installer UI
	CreateInstallWindow(baseTitle string) (NativeInstallWindow, error)
	// Should block until exit
	Main()

	// Should show an error dialog, exiting when it's closed
	ShowErrorAndQuit(err error)

	RunInMainThread(f func())
}

// A NativeInstallWindow is shown during the process. It doesn't
// have to be an actual window. It can just be text outputted to
// a terminal, for example.
type NativeInstallWindow interface {
	// Change the title of the window
	SetTitle(title string)
	// Change the label of the window
	SetLabel(label string)
	// Change the progress
	SetProgress(progress float64)
}
