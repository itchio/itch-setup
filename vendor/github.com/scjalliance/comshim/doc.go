// Package comshim provides a mechanism for maintaining an initialized
// multi-threaded component object model compartment.
//
// When working with mutli-threaded compartments, COM requires at least one
// thread to be initialized, otherwise COM-allocated resources may be released
// prematurely. This poses a challenge in Go, which can have many goroutines
// running in parallel with weak thread affinity.
//
// The comshim package provides a solution to this problem by maintaining
// a single thread-locked goroutine that has been initialized for
// multi-threaded COM use via a call to CoIntializeEx. A reference counter is
// used to determine the ongoing need for the shim to stay in place. Once the
// counter reaches 0, the thread is released and COM may be deinitialized.
//
// The comshim package is designed to allow COM-based libraries to hide the
// threading requirements of COM from the user. COM interafces can be hidden
// behind idomatic Go structures that increment the counter with calls to
// NewType() and decrement the counter with calls to Type.Close(). To see
// how this is done, take a look at the WrapperUsage example.
package comshim
