/*
 * Copyright (c) 2024-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package coreutils

import (
	"context"
	"errors"
	"sync"
	"time"

	ibus "github.com/voedger/voedger/staging/src/github.com/untillpro/airs-ibus"
)

type IResponseSender interface {
	// ErrNoConsumer
	SendResponse(any) error
}

type IResponseSenderCloseable interface {
	IResponseSender
	Close(error)
}

type IRequestSender interface {
	// err != nil -> nothing else matters
	// resultsCh must be read out
	// *resultErr must be checed only after reading out the resultCh
	// caller must eventaully close clientCtx
	SendRequest(clientCtx context.Context, req ibus.Request) (resultsCh <-chan any, resultErr *error, err error)
}

type RequestHandler func(requestCtx context.Context, request ibus.Request, responseSender IResponseSender)

type implIRequestSender struct {
	timeout        SendTimeout
	tm             ITime
	ch             chan any
	requestHandler RequestHandler
}

type SendTimeout time.Duration

type implIResponseSender struct {
	ch                 chan any
	clientCtx          context.Context
	responseStarted    chan struct{}
	responseInProgress bool
	sendTimeout        SendTimeout
	tm                 ITime
	resultErr          *error
}

func (rs *implIRequestSender) SendRequest(clientCtx context.Context, req ibus.Request) (resultsCh <-chan any, resultErr *error, err error) {
	timeoutChan := rs.tm.NewTimerChan(time.Duration(rs.timeout))
	responseSender := &implIResponseSender{
		ch:              make(chan any),
		clientCtx:       clientCtx,
		responseStarted: make(chan struct{}),
		sendTimeout:     rs.timeout,
		tm:              rs.tm,
		resultErr:       new(error),
	}
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-timeoutChan:
			err = ibus.ErrBusTimeoutExpired
		case <-responseSender.responseStarted:
			err = clientCtx.Err() // to make clientCtx.Done() take priority
		case <-clientCtx.Done():
			// wrong to close(replier.elems) because possible that elems is being writting at the same time -> data race
			// clientCxt closed -> ErrNoConsumer on SendElement() according to IReplier contract
			// so will do nothing here
			err = clientCtx.Err() // to make clientCtx.Done() take priority
		}
	}()
	rs.requestHandler(clientCtx, req, responseSender)
	wg.Wait()
	return responseSender.ch, responseSender.resultErr, err
}

func (rs *implIResponseSender) SendResponse(obj any) error {
	if !rs.responseInProgress {
		close(rs.responseStarted)
		rs.responseInProgress = true
	}
	sendTimeoutTimerChan := rs.tm.NewTimerChan(time.Duration(rs.sendTimeout))
	select {
	case rs.ch <- obj:
	case <-rs.clientCtx.Done():
	case <-sendTimeoutTimerChan:
		return ibus.ErrNoConsumer
	}
	if errors.Is(rs.clientCtx.Err(), context.Canceled) {
		return ibus.ErrNoConsumer
	}
	return rs.clientCtx.Err()
}

func (rs *implIResponseSender) Close(err error) {
	*rs.resultErr = err
	if !rs.responseInProgress {
		rs.responseInProgress = true
		close(rs.responseStarted)
	}
	close(rs.ch)
}
