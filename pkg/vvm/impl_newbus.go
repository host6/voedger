/*
 * Copyright (c) 2024-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package vvm

import (
	"context"
	"sync"
	"time"

	"github.com/voedger/voedger/pkg/coreutils"
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
	Reply() error
}

type IMultiResponder interface {
	// ErrNoConsumer
	SendElement() error
}

type IMultiResponderCloseable interface {
	IMultiResponder
	Close(error)
}

type IReplier interface {
	ISingleResponder
	IMultiResponderCloseable
}

type implIReplier struct {
	singleResponseSent   bool
	multiResponseStarted bool
	elems                chan interface{}
	tm                   coreutils.IMockTime
	sendTimeout          time.Duration
	clientCtx            context.Context
	elemsErr             *error
}

func (r *implIReplier) Reply(resp ibus.Response) error {
	if r.multiResponseStarted {
		panic("cannot send a single response if multiresponse was started already")
	}
	if r.multiResponseStarted {
		panic("can not send a single response more than once")
	}
	sendTimeoutTimerChan := r.tm.NewTimerChan(r.sendTimeout)
	select {
	case r.elems <- resp:
		r.singleResponseSent = true
		return r.clientCtx.Err() // clientCtx.Done() has priority on simultaneous (s.ctx.Done() and r.elems<- success)
	case <-r.clientCtx.Done():
		return r.clientCtx.Err()
	case <-sendTimeoutTimerChan:
		return ibus.ErrNoConsumer
	}
}

func (r *implIReplier) SendElement(elem interface{}) error {
	if r.singleResponseSent {
		panic("can not send a multi responce element after a single response is sent")
	}
	if _, ok := elem.(ibus.Response); ok {
		panic("instance of ibus.Response can not be sent as an element of multi responce")
	}
	sendTimeoutTimerChan := r.tm.NewTimerChan(r.sendTimeout)
	select {
	case r.elems <- elem:
		r.multiResponseStarted = true
		return r.clientCtx.Err() // clientCtx.Done() has priority on simultaneous (s.ctx.Done() and r.elems<- success)
	case <-r.clientCtx.Done():
		return r.clientCtx.Err()
	case <-sendTimeoutTimerChan:
		return ibus.ErrNoConsumer
	}
}

type IRequestSender interface {
	// called by router
	SendRequest(reqCtx context.Context, req ibus.Request) (resp ibus.Response, elements <-chan interface{}, errElems *error, err error)
}

type implIRequestSender struct {
	timeout        time.Duration
	tm             coreutils.IMockTime
	elems          chan interface{}
	requestHandler func(requestCtx context.Context, request ibus.Request, replier IReplier)
}

func (rs *implIRequestSender) SendRequest(clientCtx context.Context, req ibus.Request) (resp ibus.Response, elements <-chan interface{}, elemsErr *error, err error) {
	timeoutChan := rs.tm.NewTimerChan(rs.timeout)
	replier := &implIReplier{}
	handlerPanicChan := make(chan interface{}, 1)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case elemIntf := <-replier.elems:
			ok := false
			if resp, ok = elemIntf.(ibus.Response); !ok {
				elements = replier.elems
				elemsErr = replier.elemsErr
			}
			err = clientCtx.Err() // to make ctx.Done() take priority
		case <-clientCtx.Done():
			close(replier.elems)

		case <-timeoutChan:
		case <-handlerPanicChan:
		}
	}()
	rs.requestHandler(clientCtx, req, replier)
}
