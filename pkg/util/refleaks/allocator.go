// SPDX-License-Identifier: AGPL-3.0-only

package refleaks

import (
	"fmt"
	"syscall"
	"unsafe"
)

// A Reserver tracks how much memory should be reserved for an [Allocator]
// backed by a fixed amount of memory pages.
//
// Use with [Reserve].
type Reserver struct {
	bytesCap int
}

// Reserve reserves memory for the provided amount of values of type Ts.
func Reserve[T any](r *Reserver, n int) {
	var zero T
	typeSize := int(unsafe.Sizeof(zero))
	bytesCap := n * typeSize
	r.bytesCap += bytesCap
}

func (a *Reserver) pageAlignedSize() int {
	return roundUpToMultiple(a.bytesCap, pageSize)
}

func (a *Reserver) allocator() (*Allocator, []byte) {
	cap := a.pageAlignedSize()
	// Allocate separate pages for this buffer. We'll detect ref leaks by
	// munmaping the pages on Free, after which trying to access them will
	// segfault.
	b, err := syscall.Mmap(-1, 0, cap, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_PRIVATE|syscall.MAP_ANON)
	if err != nil {
		panic(fmt.Errorf("mmap: %w", err))
	}

	// Restrict the raw bytes slice to the size we actually need, without the
	// page-aligning padding. Otherwise, calling instrumentedBuf.ReaOnlyData()
	// will return uninitialized memory.
	b = b[:a.bytesCap:a.bytesCap]

	return &Allocator{b: unsafe.Pointer(unsafe.SliceData(b)), cap: len(b)}, b
}

// An Allocator allocates memory in a fixed-size memory block.
//
// Use [New] and [MakeSlice] to allocate memory in an allocator.
type Allocator struct {
	// b is a pointer to the beginning of the fixed-size arena.
	b    unsafe.Pointer
	cap  int
	used int
}

// MakeSlice allocates a slice in an [Allocator]. If the allocator is nil, it
// falls back to [builtin.make].
func MakeSlice[T any](a *Allocator, length, capacity int) []T {
	if a == nil {
		return make([]T, length, capacity)
	}

	var zero T
	typeSize := int(unsafe.Sizeof(zero))
	bytesCap := capacity * typeSize
	if left := a.cap - a.used; left < bytesCap {
		panic(fmt.Errorf("MakeSlice: tried to allocate %d bytes in an Allocator with only %d left", bytesCap, left))
	}

	sp := (*T)(unsafe.Add(a.b, a.used))
	s := unsafe.Slice(sp, capacity)
	s = s[:length]

	// Pointers must be word-aligned, so we must allocate in multiples of wordSize.
	bytesCap = ((bytesCap + wordSize - 1) / wordSize) * wordSize
	a.used += bytesCap

	return s
}

// New allocates a value in an [Allocator]. If the allocator is nil, it falls
// back to [builtin.new].
func New[T any](a *Allocator) *T {
	if a == nil {
		return new(T)
	}

	s := MakeSlice[T](a, 1, 1)
	return &s[0]
}

var pageSize = syscall.Getpagesize()

const wordSize = int(unsafe.Sizeof(uintptr(0)))
