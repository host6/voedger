# Pool

Trackable pool manages the lifetimes of reusable Go objects and their owned
children, with optional initialization, cleanup, and leak diagnostics.

## Problem

Manual pooling repeats initialization, cleanup, and release guards for
every type, especially when an object can be used alone or owned by
another object. Finding forgotten releases needs extra bookkeeping.

<details>
<summary>Without pool</summary>

The same `item` needs separate standalone and owner-only release paths,
plus repeated guards and resets.

```go
package main

import "sync"

type item struct {
	value           int
	owned, released bool
}
type owner struct {
	item     *item
	released bool
}
var items = sync.Pool{New: func() any { return &item{} }}
var owners = sync.Pool{New: func() any { return &owner{} }}
func borrowItem(owned bool) *item {
	i := items.Get().(*item)
	i.value, i.owned, i.released = 42, owned, false // Manual reset.
	return i
}
func (i *item) Release() {
	if i.owned {
		panic("must be released by owner")
	}
	i.release()
}
func (i *item) release() {
	if i.released {
		panic("already released") // Repeat this guard for every type.
	}
	i.value, i.released = 0, true // Manual cleanup.
	items.Put(i)
}
func (o *owner) Release() {
	if o.released {
		panic("already released") // More lifecycle boilerplate.
	}
	o.released = true
	o.item.release() // Easy to forget; bypasses the ownership check.
	o.item = nil
	owners.Put(o)
}
func main() {
	standalone := borrowItem(false)
	standalone.Release()
	o := owners.Get().(*owner)
	o.item, o.released = borrowItem(true), false // Manual owner init.
	o.Release() // Must explicitly release the child as well.
	// Finding forgotten releases still needs a separate tracker.
}
```

</details>

<details>
<summary>With pool</summary>

`Get()` borrows a standalone item; `GetOwned(owner)` ties the same type
to an owner's lifetime. The hooks initialize and clean up both uses.
Debug mode adds borrow-site tracking.

```go
package main

import (
	"log"
	"os"

	"github.com/voedger/voedger/pkg/goutils/pool"
)

type item struct {
	pool.IReleaser
	value int
}

// These hooks run for both standalone and owned borrows.
func (i *item) Init()    { i.value = 42 }
func (i *item) Cleanup() { i.value = 0 }

type owner struct {
	pool.IReleaser
	item *item
}

func (o *owner) Init()    { o.item = items.GetOwned(o) } // owner himself borrows owned items
func (o *owner) Cleanup() { o.item = nil }               // owned o.item is released automatically

var items = pool.NewPool[*item](func(r pool.IReleaser) any {
	return &item{IReleaser: r}
})
var owners = pool.NewPool[*owner](func(r pool.IReleaser) any {
	return &owner{IReleaser: r}
})

func main() {
	pool.SetDebug(true)
	defer pool.SetDebug(false)

	standalone := items.Get()
	standalone.Release() // Calls Cleanup; a second Release panics.

	o := owners.Get() // Init borrows an owned item automatically.
	// o.item.Release() would panic: only the owner may release it.
	o.Release() // Cleans up and releases both owner and child.

	if pool.GetObjectsInUse() != 0 {
		log.Println("Objects were borrowed but not released:")
		pool.PrintNonReleased(os.Stdout)
	}
}
```

</details>

## Features

- **Object lifecycle** - Initialize, clean up, and guard release
  - [Pool construction: impl.go#L163](impl.go#L163)
  - [Release contract: interface.go#L20](interface.go#L20)
- **Owned lifetimes** - Release children with their owner
  - [Owned borrowing: interface.go#L14](interface.go#L14)
  - [Owner release: interface.go#L25](interface.go#L25)
- **Usage tracking** - Count outstanding borrows across pools
  - [Global count: utils.go#L16](utils.go#L16)
  - [Counter registration: utils.go#L35](utils.go#L35)
- **Leak diagnostics** - Locate outstanding borrows by call site
  - [Debug configuration: utils.go#L60](utils.go#L60)
  - [Leak report: utils.go#L43](utils.go#L43)
- **[Pool stub](impl.go#L156)** - Create fresh objects for debugging

## Use

See the [basic usage test](impl_test.go#L41) and
[owned objects test](owned_test.go#L66) for complete examples.

Embed `pool.IReleaser` in each pooled struct and initialize it with the
releaser passed to the factory. Return a pointer to that struct.
Optional `Init()` runs on every `Get()` and `GetOwned()`; `Cleanup()`
runs during release, before owned children are released. Reset
application fields in these hooks as needed.

Use `GetOwned(owner)` for children whose lifetime follows their owner.
Calling `Release()` on an owned object panics; releasing the owner
automatically releases its children and their owned descendants.
Do not release owned children in `Cleanup()`. After release, do not
access the object or any of its fields. For manual-pool pitfalls, see
the [double-release example](pool_wrong_test.go#L45).

Add `require.Zero(t, pool.GetObjectsInUse())` after releasing all
objects in tests. The total includes standalone and owned objects,
even with debugging disabled. Use `pool.RegisterObjectsInUseCounter`
to include other pools. Callbacks must be thread-safe and may call
`pool.PrintNonReleased`. Counters remain registered so discarding a pool
does not hide its unreleased objects.

For leak investigations, enable `pool.SetDebug(true)` before borrowing.
Disabling it with `pool.SetDebug(false)` stops recording new borrows;
already recorded traces remain visible until their objects are released.
Release removes those traces even while debugging is disabled. Print
outstanding borrow sites with `pool.PrintNonReleased(os.Stdout)`.
Stack traces are captured for `Get()` calls; owned borrows contribute
to the total count without separate traces.
`SetDebug()` supports concurrent calls during borrowing and release.
Debugging adds overhead; see the [debugging test](impl_test.go#L70).

Replace `NewPool()` with `NewPoolStub()` to create a fresh object on
every borrow when investigating reuse issues. Initialization, cleanup,
release guards, ownership, and usage counts still apply. See the
[stub test](impl_test.go#L105).
