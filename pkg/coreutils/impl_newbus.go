/*
 * Copyright (c) 2024-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package coreutils

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	ibus "github.com/voedger/voedger/staging/src/github.com/untillpro/airs-ibus"
)

type ICmdResponseReplier interface {
	// panics if called >1 times
	Reply(ibus.Response)
}

type IQryReplier interface {
	// ibus.ErrNoConsumer -> further communication senseless
	SendElement(elem interface{}) error
}

type ISingleResponder interface {
	// ErrNoConsumer
	Reply(ibus.Response) error
}

type IElementsSender interface {
	// ErrNoConsumer
	SendElement(any) error
}

type IMultiResponderCloseable interface {
	IElementsSender
	Close(error)
}

type IReplier interface {
	ISingleResponder
	IMultiResponderCloseable
}

type implIReplier struct {
	singleResponseSent    bool
	marshaledElems        chan string
	singleResponse        chan ibus.Response
	multiResponseStarted  chan struct{}
	bMultiResponseStarted bool
	tm                    ITime
	sendTimeout           SendTimeout
	clientCtx             context.Context
	elemsErr              *error
}

func (r *implIReplier) Reply(resp ibus.Response) error {
	if r.bMultiResponseStarted {
		panic("cannot send a single response if multi response was started already")
	}
	if r.singleResponseSent {
		panic("can not send a single response more than once")
	}
	sendTimeoutTimerChan := r.tm.NewTimerChan(time.Duration(r.sendTimeout))
	select {
	case r.singleResponse <- resp:
		r.singleResponseSent = true
		return r.clientCtx.Err() // clientCtx.Done() has priority on simultaneous (s.ctx.Done() and r.elems<- success)
	case <-r.clientCtx.Done():
		return r.clientCtx.Err()
	case <-sendTimeoutTimerChan:
		return ibus.ErrNoConsumer
	}
}

func (r *implIReplier) SendElement(elem any) error {
	if r.singleResponseSent {
		panic("can not send a multi response element after a single response is sent")
	}
	if _, ok := elem.(ibus.Response); ok {
		panic("instance of ibus.Response can not be sent as an element of multi response")
	}
	marshaledElem, err := json.Marshal(&elem)
	if err != nil {
		return err
	}
	sendTimeoutTimerChan := r.tm.NewTimerChan(time.Duration(r.sendTimeout))
	select {
	case r.marshaledElems <- string(marshaledElem):
		r.bMultiResponseStarted = true
		close(r.multiResponseStarted)
		return r.clientCtx.Err() // clientCtx.Done() has priority on simultaneous (s.ctx.Done() and r.elems<- success)
	case <-r.clientCtx.Done():
		return r.clientCtx.Err()
	case <-sendTimeoutTimerChan:
		return ibus.ErrNoConsumer
	}
}

func (r *implIReplier) Close(err error) {
	*r.elemsErr = err
	close(r.marshaledElems)
}

type IRequestSender interface {
	// called by router
	SendRequest(reqCtx context.Context, req ibus.Request) (resp ibus.Response, marshaledElems <-chan string, errElems *error, err error)
}

type RequestHandler func(requestCtx context.Context, request ibus.Request, replier IReplier)

type SendTimeout time.Duration

func NewIRequestSender(requestHandler RequestHandler, tm ITime, timeout SendTimeout) IRequestSender {
	return &implIRequestSender{
		timeout:        timeout,
		tm:             tm,
		requestHandler: requestHandler,
		elems:          make(chan any),
	}
}

type implIRequestSender struct {
	timeout        SendTimeout
	tm             ITime
	elems          chan any
	requestHandler func(requestCtx context.Context, request ibus.Request, replier IReplier)
}

func (rs *implIRequestSender) SendRequest(clientCtx context.Context, req ibus.Request) (resp ibus.Response, marshaledElems <-chan string, elemsErr *error, err error) {
	timeoutChan := rs.tm.NewTimerChan(time.Duration(rs.timeout))
	replier := &implIReplier{
		marshaledElems:       make(chan string),
		singleResponse:       make(chan ibus.Response),
		tm:                   rs.tm,
		sendTimeout:          rs.timeout,
		clientCtx:            clientCtx,
		elemsErr:             elemsErr,
		multiResponseStarted: make(chan struct{}),
	}
	handlerPanicChan := make(chan interface{}, 1)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case resp = <-replier.singleResponse:
			err = clientCtx.Err() // to make clientCtx.Done() take priority
		case <-replier.multiResponseStarted:
			marshaledElems = replier.marshaledElems
			elemsErr = replier.elemsErr
			err = clientCtx.Err() // to make clientCtx.Done() take priority
		case <-clientCtx.Done():
			// wrong to close(replier.elems) because possible that elems is being writting at the same time -> data race
			// clientCxt closed -> ErrNoConsumer on SendElement() according to IReplier contract
			// so will do nothing here
		case <-timeoutChan:
		case <-handlerPanicChan:
		}
	}()
	rs.requestHandler(clientCtx, req, replier)
	wg.Wait()
	return resp, marshaledElems, elemsErr, err
}
