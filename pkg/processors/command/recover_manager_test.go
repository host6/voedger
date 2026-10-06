/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 */

package commandprocessor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRecoverManager_BasicUsage(t *testing.T) {
	require := require.New(t)
	manager, scheduled, started, completed := newTestRecoverManager(nil)
	gate := make(chan struct{})
	factoryCalls := 0
	value := &recoverManagerTestValue{id: 1}

	recovered, err := manager.getOrStart(context.Background(), 10,
		func() recoveryAttemptFunc[recoverManagerTestValue] {
			factoryCalls++
			return func(context.Context) (*recoverManagerTestValue, error) {
				<-gate
				return value, nil
			}
		})
	require.Nil(recovered)
	require.ErrorIs(err, errRecoveryInProgress)
	require.Equal(10, receiveRecoverManagerTestValue(t, scheduled))
	require.Equal(10, receiveRecoverManagerTestValue(t, started))

	recovered, err = manager.getOrStart(context.Background(), 10,
		func() recoveryAttemptFunc[recoverManagerTestValue] {
			factoryCalls++
			return nil
		})
	require.Nil(recovered)
	require.ErrorIs(err, errRecoveryInProgress)
	require.Equal(1, factoryCalls)

	close(gate)
	completion := receiveRecoverManagerTestValue(t, completed)
	require.Equal(10, completion.key)
	require.NoError(completion.err)

	recovered, err = manager.getOrStart(context.Background(), 10,
		func() recoveryAttemptFunc[recoverManagerTestValue] {
			factoryCalls++
			return nil
		})
	require.NoError(err)
	require.Same(value, recovered)
	require.Equal(1, factoryCalls)
	require.Equal(map[int]*recoverManagerTestValue{10: value}, manager.recoveredValues())
	manager.shutdown()
	require.Empty(manager.recoveredValues())
}

type recoverManagerTestValue struct {
	id int
}

type recoverManagerTestCompletion struct {
	key int
	err error
}

func newTestRecoverManager(slots chan struct{}) (
	manager *recoverManager[int, recoverManagerTestValue],
	scheduled chan int,
	started chan int,
	completed chan recoverManagerTestCompletion,
) {
	scheduled = make(chan int, 10)
	started = make(chan int, 10)
	completed = make(chan recoverManagerTestCompletion, 10)
	manager = newRecoverManager[int, recoverManagerTestValue](slots, recoveryHooks[int]{
		scheduled: func(key int) {
			scheduled <- key
		},
		beforeAttempt: func(_ context.Context, key int) error {
			started <- key
			return nil
		},
		attemptCompleted: func(key int, err error) {
			completed <- recoverManagerTestCompletion{key: key, err: err}
		},
	})
	return
}

func receiveRecoverManagerTestValue[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for recover manager")
		var zero T
		return zero
	}
}

func TestRecoverManager(t *testing.T) {

	t.Run("retains a failure and starts exactly one retry", func(t *testing.T) {
		require := require.New(t)
		manager, _, _, completed := newTestRecoverManager(nil)
		failure := errors.New("recovery failed")

		recovered, err := manager.getOrStart(context.Background(), 1,
			func() recoveryAttemptFunc[recoverManagerTestValue] {
				return func(context.Context) (*recoverManagerTestValue, error) {
					return nil, failure
				}
			})
		require.Nil(recovered)
		require.ErrorIs(err, errRecoveryInProgress)
		require.ErrorIs(receiveRecoverManagerTestValue(t, completed).err, failure)

		retryGate := make(chan struct{})
		retryStarted := make(chan struct{})
		retryValue := &recoverManagerTestValue{id: 2}
		retryFactories := 0
		recovered, err = manager.getOrStart(context.Background(), 1,
			func() recoveryAttemptFunc[recoverManagerTestValue] {
				retryFactories++
				return func(context.Context) (*recoverManagerTestValue, error) {
					close(retryStarted)
					<-retryGate
					return retryValue, nil
				}
			})
		require.Nil(recovered)
		require.ErrorIs(err, failure)
		receiveRecoverManagerTestValue(t, retryStarted)

		recovered, err = manager.getOrStart(context.Background(), 1,
			func() recoveryAttemptFunc[recoverManagerTestValue] {
				retryFactories++
				return nil
			})
		require.Nil(recovered)
		require.ErrorIs(err, errRecoveryInProgress)
		require.Equal(1, retryFactories)

		close(retryGate)
		require.NoError(receiveRecoverManagerTestValue(t, completed).err)
		recovered, err = manager.getOrStart(context.Background(), 1, nil)
		require.NoError(err)
		require.Same(retryValue, recovered)
		manager.shutdown()
	})

	t.Run("does not create an attempt when the recovery limit is reached", func(t *testing.T) {
		require := require.New(t)
		manager, _, started, completed := newTestRecoverManager(make(chan struct{}, 1))
		gate := make(chan struct{})

		_, err := manager.getOrStart(context.Background(), 1,
			func() recoveryAttemptFunc[recoverManagerTestValue] {
				return func(context.Context) (*recoverManagerTestValue, error) {
					<-gate
					return &recoverManagerTestValue{id: 1}, nil
				}
			})
		require.ErrorIs(err, errRecoveryInProgress)
		require.Equal(1, receiveRecoverManagerTestValue(t, started))

		factoryCalled := false
		recovered, err := manager.getOrStart(context.Background(), 2,
			func() recoveryAttemptFunc[recoverManagerTestValue] {
				factoryCalled = true
				return nil
			})
		require.Nil(recovered)
		require.ErrorIs(err, errRecoveryLimit)
		require.False(factoryCalled)

		close(gate)
		require.NoError(receiveRecoverManagerTestValue(t, completed).err)
		manager.shutdown()

		zeroLimitManager, _, _, _ := newTestRecoverManager(make(chan struct{}))
		_, err = zeroLimitManager.getOrStart(context.Background(), 1,
			func() recoveryAttemptFunc[recoverManagerTestValue] {
				factoryCalled = true
				return nil
			})
		require.ErrorIs(err, errRecoveryLimit)
		require.False(factoryCalled)
		zeroLimitManager.shutdown()
	})

	t.Run("a failed value cannot retry until a recovery slot is available", func(t *testing.T) {
		require := require.New(t)
		manager, _, started, completed := newTestRecoverManager(make(chan struct{}, 1))
		failure := errors.New("first attempt failed")

		_, err := manager.getOrStart(context.Background(), 1,
			func() recoveryAttemptFunc[recoverManagerTestValue] {
				return func(context.Context) (*recoverManagerTestValue, error) {
					return nil, failure
				}
			})
		require.ErrorIs(err, errRecoveryInProgress)
		require.Equal(1, receiveRecoverManagerTestValue(t, started))
		require.ErrorIs(receiveRecoverManagerTestValue(t, completed).err, failure)

		otherGate := make(chan struct{})
		_, err = manager.getOrStart(context.Background(), 2,
			func() recoveryAttemptFunc[recoverManagerTestValue] {
				return func(context.Context) (*recoverManagerTestValue, error) {
					<-otherGate
					return &recoverManagerTestValue{id: 2}, nil
				}
			})
		require.ErrorIs(err, errRecoveryInProgress)
		require.Equal(2, receiveRecoverManagerTestValue(t, started))

		retryFactoryCalled := false
		_, err = manager.getOrStart(context.Background(), 1,
			func() recoveryAttemptFunc[recoverManagerTestValue] {
				retryFactoryCalled = true
				return nil
			})
		require.ErrorIs(err, errRecoveryLimit)
		require.False(retryFactoryCalled)

		close(otherGate)
		require.NoError(receiveRecoverManagerTestValue(t, completed).err)
		retryValue := &recoverManagerTestValue{id: 3}
		_, err = manager.getOrStart(context.Background(), 1,
			func() recoveryAttemptFunc[recoverManagerTestValue] {
				retryFactoryCalled = true
				return func(context.Context) (*recoverManagerTestValue, error) {
					return retryValue, nil
				}
			})
		require.ErrorIs(err, failure)
		require.True(retryFactoryCalled)
		require.NoError(receiveRecoverManagerTestValue(t, completed).err)
		recovered, err := manager.getOrStart(context.Background(), 1, nil)
		require.NoError(err)
		require.Same(retryValue, recovered)
		manager.shutdown()
	})

	t.Run("before-attempt and context errors are retained", func(t *testing.T) {
		require := require.New(t)
		beforeFailure := errors.New("before attempt failed")
		beforeCompleted := make(chan error, 1)
		attemptCalled := false
		beforeManager := newRecoverManager[int, recoverManagerTestValue](nil, recoveryHooks[int]{
			scheduled: func(int) {},
			beforeAttempt: func(context.Context, int) error {
				return beforeFailure
			},
			attemptCompleted: func(_ int, err error) {
				beforeCompleted <- err
			},
		})
		_, err := beforeManager.getOrStart(context.Background(), 1,
			func() recoveryAttemptFunc[recoverManagerTestValue] {
				return func(context.Context) (*recoverManagerTestValue, error) {
					attemptCalled = true
					return &recoverManagerTestValue{id: 1}, nil
				}
			})
		require.ErrorIs(err, errRecoveryInProgress)
		require.ErrorIs(receiveRecoverManagerTestValue(t, beforeCompleted), beforeFailure)
		require.False(attemptCalled)
		beforeManager.shutdown()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		contextCompleted := make(chan error, 1)
		contextManager := newRecoverManager[int, recoverManagerTestValue](nil, recoveryHooks[int]{
			scheduled: func(int) {},
			beforeAttempt: func(context.Context, int) error {
				return nil
			},
			attemptCompleted: func(_ int, err error) {
				contextCompleted <- err
			},
		})
		_, err = contextManager.getOrStart(ctx, 1,
			func() recoveryAttemptFunc[recoverManagerTestValue] {
				return func(context.Context) (*recoverManagerTestValue, error) {
					return &recoverManagerTestValue{id: 1}, nil
				}
			})
		require.ErrorIs(err, errRecoveryInProgress)
		require.ErrorIs(receiveRecoverManagerTestValue(t, contextCompleted), context.Canceled)
		contextManager.shutdown()
	})

	t.Run("a stale completion cannot overwrite state created after reset", func(t *testing.T) {
		require := require.New(t)
		manager, _, started, completed := newTestRecoverManager(nil)
		oldGate := make(chan struct{})
		newGate := make(chan struct{})
		oldValue := &recoverManagerTestValue{id: 1}
		newValue := &recoverManagerTestValue{id: 2}
		var closeOld sync.Once
		var closeNew sync.Once
		defer func() {
			closeOld.Do(func() { close(oldGate) })
			closeNew.Do(func() { close(newGate) })
			manager.shutdown()
		}()

		_, err := manager.getOrStart(context.Background(), 1,
			func() recoveryAttemptFunc[recoverManagerTestValue] {
				return func(context.Context) (*recoverManagerTestValue, error) {
					<-oldGate
					return oldValue, nil
				}
			})
		require.ErrorIs(err, errRecoveryInProgress)
		require.Equal(1, receiveRecoverManagerTestValue(t, started))

		manager.reset(1)
		_, err = manager.getOrStart(context.Background(), 1,
			func() recoveryAttemptFunc[recoverManagerTestValue] {
				return func(context.Context) (*recoverManagerTestValue, error) {
					<-newGate
					return newValue, nil
				}
			})
		require.ErrorIs(err, errRecoveryInProgress)
		require.Equal(1, receiveRecoverManagerTestValue(t, started))

		closeOld.Do(func() { close(oldGate) })
		require.NoError(receiveRecoverManagerTestValue(t, completed).err)
		recovered, err := manager.getOrStart(context.Background(), 1,
			func() recoveryAttemptFunc[recoverManagerTestValue] {
				t.Fatal("stale completion incorrectly made a new attempt possible")
				return nil
			})
		require.Nil(recovered)
		require.ErrorIs(err, errRecoveryInProgress)

		closeNew.Do(func() { close(newGate) })
		require.NoError(receiveRecoverManagerTestValue(t, completed).err)
		recovered, err = manager.getOrStart(context.Background(), 1, nil)
		require.NoError(err)
		require.Same(newValue, recovered)
	})
}
