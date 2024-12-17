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

type IResponder interface {
	// panics if called >1 times
	InitResponse(ResponseMeta) IResponseSenderCloseable
}

type IResponseSender interface {
	// ErrNoConsumer
	Send(any) error
}

type IResponseSenderCloseable interface {
	IResponseSender
	Close(error)
}

type ResponseMeta struct {
	ContentType string
	StatusCode  int
}

type IRequestSender interface {
	// err != nil -> nothing else matters
	// resultsCh must be read out
	// *resultErr must be checked only after reading out the resultCh
	// caller must eventaully close clientCtx
	SendRequest(clientCtx context.Context, req ibus.Request) (responseCh <-chan any, responseMeta ResponseMeta, responseErr *error, err error)
}

type RequestHandler func(requestCtx context.Context, request ibus.Request, responder IResponder)

type implIRequestSender struct {
	timeout        SendTimeout
	tm             ITime
	requestHandler RequestHandler
}

type SendTimeout time.Duration

type implIResponseSenderCloseable struct {
	ch          chan any
	clientCtx   context.Context
	sendTimeout SendTimeout
	tm          ITime
	resultErr   *error
}

type implIResponder struct {
	respSender     IResponseSenderCloseable
	inited         bool
	responseMetaCh chan ResponseMeta
}

func NewIRequestSender(tm ITime, sendTimeout SendTimeout, requestHandler RequestHandler) IRequestSender {
	return &implIRequestSender{
		timeout:        sendTimeout,
		tm:             tm,
		requestHandler: requestHandler,
	}
}

func (rs *implIRequestSender) SendRequest(clientCtx context.Context, req ibus.Request) (responseCh <-chan any, responseMeta ResponseMeta, responseErr *error, err error) {
	timeoutChan := rs.tm.NewTimerChan(time.Duration(rs.timeout))
	respSender := &implIResponseSenderCloseable{
		ch:          make(chan any),
		clientCtx:   clientCtx,
		sendTimeout: rs.timeout,
		tm:          rs.tm,
		resultErr:   new(error),
	}
	responder := &implIResponder{
		respSender:     respSender,
		responseMetaCh: make(chan ResponseMeta, 1),
	}
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-timeoutChan:
			err = ibus.ErrBusTimeoutExpired
		case responseMeta = <-responder.responseMetaCh:
			err = clientCtx.Err() // to make clientCtx.Done() take priority
		case <-clientCtx.Done():
			// wrong to close(replier.elems) because possible that elems is being writting at the same time -> data race
			// clientCxt closed -> ErrNoConsumer on SendElement() according to IReplier contract
			// so will do nothing here
			err = clientCtx.Err() // to make clientCtx.Done() take priority
		}
	}()
	rs.requestHandler(clientCtx, req, responder)
	wg.Wait()
	return respSender.ch, responseMeta, respSender.resultErr, err
}

func (rs *implIResponseSenderCloseable) Send(obj any) error {
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

func (rs *implIResponseSenderCloseable) Close(err error) {
	*rs.resultErr = err
	close(rs.ch)
}

func (r *implIResponder) InitResponse(rm ResponseMeta) IResponseSenderCloseable {
	select {
	case r.responseMetaCh <- rm:
	default:
		panic(ibus.ErrNoConsumer)
	}
	return r.respSender
}
