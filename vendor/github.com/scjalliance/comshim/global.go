package comshim

var global = New()

// Add adds delta, which may be negative, to the counter of a global shim. As
// long as the counter is greater than zero, at least one thread is guaranteed
// to be initialized for mutli-threaded COM access.
//
// If the counter becomes zero, the shim is released and COM resources may be
// released if there are no other threads that are still initialized.
//
// If the counter goes negative, Add panics.
//
// If the shim cannot be created for some reason, Add panics.
func Add(delta int) {
	global.Add(delta)
}

// Done decrements the counter of a global shim.
func Done() {
	global.Done()
}
