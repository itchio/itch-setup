package comshim

import (
	"sync/atomic"
	"unsafe"
)

// Counter wraps an int64 atomic counter in a way that provides proper byte
// alignment.
type Counter struct {
	data [12]byte // Allocate 12 bytes and then use whichever 8 are properly aligned
}

// Add will add the given delta value, which may be negative, to the atomic
// counter and return the new value.
func (c *Counter) Add(delta int64) int64 {
	valuep := c.addr()
	return atomic.AddInt64(valuep, delta)
}

// Value returns the current value of the counter.
func (c *Counter) Value() int64 {
	valuep := c.addr()
	return atomic.LoadInt64(valuep)
}

// addr returns the 64-bit aligned address of the counter's data. The alignment
// is necessary for 64-bit operations on 32-bit compilers.
func (c *Counter) addr() *int64 {
	if uintptr(unsafe.Pointer(&c.data))%8 == 0 {
		return (*int64)(unsafe.Pointer(&c.data))
	}
	return (*int64)(unsafe.Pointer(&c.data[4]))
}
