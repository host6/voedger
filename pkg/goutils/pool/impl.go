/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package pool

import (
	"bytes"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

var (
	m               sync.Mutex = sync.Mutex{}
	objectsCounters []func() uint64
	isDebug         atomic.Bool
	objAmounts      = map[string]int{}
)

func (st stackTrace) string() string {
	buf := bytes.NewBufferString("")
	for _, sf := range st {
		fmt.Fprintf(buf, "%s\n\t%s:%d\n", sf.fn, sf.file, sf.line)
	}
	return buf.String()
}

func (p *implPool[T]) Get() T {
	obj := p.get()
	releaseable := obj.(IReleaser)
	releaseable.reset()
	releaseable.init(obj)
	p.objectsInUse.Add(1)
	if isDebug.Load() {
		st := getStackTrace().string()
		releaseable.setBorrowStackTrace(st)
		m.Lock()
		count := objAmounts[st]
		count++
		objAmounts[st] = count
		m.Unlock()
	}
	return obj.(T)
}

func (p *implPool[T]) get() any {
	var obj any
	if p.isStub {
		releaser := &implIReleaser[T]{
			ownerPool: p,
		}
		obj = p.instantiator(releaser)
		releaser.cleanupIntf, _ = obj.(interface{ Cleanup() })
		releaser.obj = obj.(T)
	} else {
		obj = p.Pool.Get()
	}
	return obj
}

func (p *implPool[T]) GetOwned(owner IReleaser) T {
	obj := p.get()
	p.objectsInUse.Add(1)
	releaseable := obj.(IReleaser)
	releaseable.reset()
	releaseable.setIsOwned()
	releaseable.setOwnedTail(owner.getOwnedTail())
	owner.setOwnedTail(releaseable)
	releaseable.init(obj)
	return obj.(T)
}

func (p *implPool[T]) GetObjectsInUse() uint64 {
	return p.objectsInUse.Load()
}

func (r *implIReleaser[T]) Release() {
	if r.isOwned {
		panic("must be released by owner")
	}
	r.releaseOwned()
}

func (r *implIReleaser[T]) reset() {
	r.releaseStarted.Store(false)
	r.isOwned = false
	r.borrowStackTrace = ""
}

func (r *implIReleaser[T]) IsOwned() bool {
	return r.isOwned
}

func (r *implIReleaser[T]) setIsOwned() {
	r.isOwned = true
}

func (r *implIReleaser[T]) setBorrowStackTrace(stackTrace string) {
	r.borrowStackTrace = stackTrace
}

func (r *implIReleaser[T]) releaseOwned() {
	// Claim release before invoking cleanup or releasing owned objects.
	if !r.releaseStarted.CompareAndSwap(false, true) {
		panic("already released")
	}
	if r.cleanupIntf != nil {
		r.cleanupIntf.Cleanup()
	}
	if r.ownedTail != nil {
		r.ownedTail.(IReleaser).releaseOwned()
		r.ownedTail = nil
	}
	r.ownerPool.objectsInUse.Add(^uint64(0))
	// Remove the trace recorded for this borrow even if debug mode was
	// disabled after Get.
	if st := r.borrowStackTrace; st != "" {
		m.Lock()
		objAmounts[st]--
		if objAmounts[st] == 0 {
			delete(objAmounts, st)
		}
		m.Unlock()
		r.borrowStackTrace = ""
	}
	if !r.ownerPool.isStub {
		r.ownerPool.Put(r.obj)
	}
}

func (r *implIReleaser[T]) init(obj interface{}) {
	if !r.isInitIntfDetermined {
		r.initIntf, _ = obj.(interface{ Init() })
		r.isInitIntfDetermined = true
	}
	if r.initIntf != nil {
		r.initIntf.Init()
	}
}

func (r *implIReleaser[T]) setOwnedTail(tail interface{}) {
	r.ownedTail = tail
}

func (r *implIReleaser[T]) getOwnedTail() interface{} {
	return r.ownedTail
}

// NewPoolStub creates pool which does not act as a pool. I.e. just creates a new instance on each Get()
// Release() does nothing more but Cleaunp() call if it exists
// does not track borrow source code points in debug mode
// useful for investigations
func NewPoolStub[T any](instantiator func(releaser IReleaser) any) IPool[T] {
	res := newPool[T](instantiator)
	res.instantiator = instantiator
	res.isStub = true
	return res
}

func NewPool[T any](instantiator func(releaser IReleaser) any) IPool[T] {
	res := newPool[T](nil)
	res.Pool = sync.Pool{
		New: func() interface{} {
			releaser := &implIReleaser[T]{
				ownerPool: res,
			}
			newInstance := instantiator(releaser)
			releaser.cleanupIntf, _ = newInstance.(interface{ Cleanup() })
			releaser.obj = newInstance.(T)
			return newInstance
		},
	}
	return res
}

func newPool[T any](instantiator func(releaser IReleaser) any) *implPool[T] {
	res := &implPool[T]{instantiator: instantiator}
	RegisterObjectsInUseCounter(func() uint64 { return res.GetObjectsInUse() })
	return res
}

func getStackTrace() stackTrace {
	const (
		maxStackDepth     = 100
		stackFramesToSkip = 3 // Skip runtime.Callers, getStackTrace, and implPool.Get.
	)
	pc := make([]uintptr, maxStackDepth)
	n := runtime.Callers(stackFramesToSkip, pc)
	frames := runtime.CallersFrames(pc[:n])
	st := stackTrace{}
	for {
		frame, more := frames.Next()
		st = append(st, stackFrame{
			fn:   frame.Function,
			file: frame.File,
			line: frame.Line,
		})
		if !more {
			break
		}
	}
	return st
}
