/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package pool_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/goutils/pool"
)

func BenchmarkBasic(b *testing.B) {
	p := pool.NewPool[*myStruct](func(releaser pool.IReleaser) any {
		return &myStruct{IReleaser: releaser}
	})

	b.Run("basic", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			myStructInstance := p.Get()
			myStructInstance.Release()
		}
	})
}

type simpleStruct struct {
	pool.IReleaser
	isReleased bool
}

// BenchmarkExample/pool-4        20090349	        71.98 ns/op	       0 B/op	       0 allocs/op
// BenchmarkExample/sync.Pool-4   39997732	        27.70 ns/op	       0 B/op	       0 allocs/op
func BenchmarkExample(b *testing.B) {
	p := pool.NewPool[*simpleStruct](func(releaser pool.IReleaser) any { return &simpleStruct{IReleaser: releaser} })

	b.Run("pool", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			ps := p.Get()
			ps.Release()
		}
	})

	b.Run("sync.Pool", func(b *testing.B) {
		syncPool := sync.Pool{
			New: func() interface{} {
				return &simpleStruct{}
			},
		}
		objectsInUse := uint64(0)

		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			obj := syncPool.Get().(*simpleStruct)
			atomic.AddUint64(&objectsInUse, uint64(1))
			obj.isReleased = false

			if obj.isReleased {
				panic("already released")
			}
			syncPool.Put(obj)
			atomic.AddUint64(&objectsInUse, ^uint64(0))
		}
	})

	require.Zero(b, pool.GetObjectsInUse())
}
