/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package pool_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/valyala/bytebufferpool"
	"github.com/voedger/voedger/pkg/goutils/pool"
)

type owner struct {
	pool.IReleaser
	nested *nested
	bb     *bytebufferpool.ByteBuffer
}

type nested struct {
	pool.IReleaser
	internal *internal
	bb       *bytebufferpool.ByteBuffer
}

type internal struct {
	pool.IReleaser
}

func (n *nested) Init() {
	n.internal = poolInternal.GetOwned(n)
	n.bb = bytebufferpool.Get()
}

func (n *nested) Cleanup() {
	bytebufferpool.Put(n.bb)
	n.bb = nil
}

func (o *owner) Init() {
	// lifetime of owner.nested must not be shorter than owner's so let's obtain *nested by GetOwned()
	// so that it will be released automatically on owner.Release()
	o.nested = poolNested.GetOwned(o)
	o.bb = bytebufferpool.Get()
}

func (o *owner) Cleanup() {
	bytebufferpool.Put(o.bb)
	o.bb = nil
}

var (
	poolOwner = pool.NewPool[*owner](func(releaser pool.IReleaser) any {
		return &owner{IReleaser: releaser}
	})
	poolNested = pool.NewPool[*nested](func(releaser pool.IReleaser) any {
		return &nested{IReleaser: releaser}
	})
	poolInternal = pool.NewPool[*internal](func(releaser pool.IReleaser) any {
		return &internal{IReleaser: releaser}
	})
)

func TestBasicUsage_Owned(t *testing.T) {
	require := require.New(t)

	owner := poolOwner.Get()

	// `nested` and `internal` objects are taken from theirs pools on owner borrow following theirs Init() methods
	require.Equal(uint64(3), pool.GetObjectsInUse())
	require.NotNil(owner.bb)
	require.NotNil(owner.nested.bb)
	require.NotNil(owner.nested.internal)

	// `nested` type is used in both owned and standalone ways
	nestedStandalone := poolNested.Get()

	// unable to release objects borrowed by GetOwned()
	// theirs lifetime is not under developer's control
	// it will be released automatically on owner.Release() to prevent the released owner.nested usage
	require.Panics(func() { owner.nested.Release() })
	require.Panics(func() { owner.nested.internal.Release() })

	// ok to release the standalone object
	nestedStandalone.Release()

	require.False(owner.IsOwned())
	require.True(owner.nested.IsOwned())
	require.True(owner.nested.internal.IsOwned())

	owner.Release()
	// owner is released, nested is released automatically as well
	// neither owner, nor any of its fields, nor owner.nested itself and its fields must not be touched from now on

	require.Equal(uint64(0), pool.GetObjectsInUse())
}

func BenchmarkOwned(b *testing.B) {
	b.Run("basic", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			owner := poolOwner.Get()
			owner.Release()
		}
	})
}
