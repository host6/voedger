/*
 * Copyright (c) 2024-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package bus

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/voedger/voedger/pkg/coreutils"
	"github.com/voedger/voedger/pkg/goutils/logger"
)

func (rs *implIRequestSender) SendRequest(clientCtx context.Context, req Request) (responseCh <-chan any, responseMeta ResponseMeta, responseErr *error, err error) {
	timeoutChan := rs.tm.NewTimerChan(time.Duration(rs.timeout))
	respWriter := &implResponseWriter{
		ch:          make(chan IChunk, 1), // buf size 1 to make single write on Respond()
		clientCtx:   clientCtx,
		sendTimeout: rs.timeout,
		tm:          rs.tm,
		resultErr:   new(error),
	}
	responder := &implIResponder{
		respWriter:     respWriter,
		responseMetaCh: make(chan ResponseMeta, 1),
	}
	handlerPanic := make(chan interface{})
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer func() {
			wg.Done()
		}()
		select {
		case <-timeoutChan:
			if err = checkHandlerPanic(handlerPanic); err == nil {
				err = ErrSendTimeoutExpired
			}
		case responseMeta = <-responder.responseMetaCh:
			err = clientCtx.Err() // to make clientCtx.Done() take priority
		case <-clientCtx.Done():
			// wrong to close(replier.elems) because possible that elems is being writing at the same time -> data race
			// clientCxt closed -> ErrNoConsumer on SendElement() according to IReplier contract
			// so will do nothing here
			if err = checkHandlerPanic(handlerPanic); err == nil {
				err = clientCtx.Err() // to make clientCtx.Done() take priority
			}
		case panicIntf := <-handlerPanic:
			err = handlePanic(panicIntf)
		}
	}()
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("handler panic:", fmt.Sprint(r), "\n", string(debug.Stack()))
				// will process panic in the goroutine instead of update err here to avoid data race
				// https://dev.untill.com/projects/#!607751
				handlerPanic <- r
			}
		}()
		rs.requestHandler(clientCtx, req, responder)
	}()
	wg.Wait()
	return respWriter.ch, responseMeta, respWriter.resultErr, err
}

func checkHandlerPanic(ch <-chan interface{}) error {
	select {
	case r := <-ch:
		return handlePanic(r)
	default:
		return nil
	}
}

func handlePanic(r interface{}) error {
	switch rTyped := r.(type) {
	case string:
		return errors.New(rTyped)
	case error:
		return rTyped
	default:
		// notest
		return fmt.Errorf("%#v", r)
	}
}

func (r *implIResponder) InitResponse(statusCode int) IResponseWriter {
	r.checkStarted()
	select {
	case r.responseMetaCh <- ResponseMeta{ContentType: coreutils.ApplicationJSON, StatusCode: statusCode}:
	default:
		// do nothing if no consumer already.
		// will get ErrNoConsumer on the next Write()
	}
	return r.respWriter
}

func newIChunk(doneChan chan error) IChunk {
	return chunk{doneChan: doneChan}
}

func (r *implIResponder) Respond(responseMeta ResponseMeta, obj any) error {
	r.checkStarted()
	if responseMeta.mode != 0 {
		panic("responseMeta.mode is set by someone else!")
	}
	responseMeta.mode = RespondMode_Single
	select {
	case r.responseMetaCh <- responseMeta:
		// TODO: rework here: possible: http client disconnected, write to r.respWriter.ch successful, we're thinking that we're replied, but it is not, no socket to write to

		r.respWriter.ch <- obj // buf size 1
		close(r.respWriter.ch)
	default:
		return ErrNoConsumer
	}
	return nil
}

/*
только тут надо следить за клиентским контекстом, т.к. если роутер перестал читать канал, то мы тут не отличим
если роутер следит за контекстом:
невозможно использовать for  := range ch
ну пусть не получится. тогда в роутере:
for {
	select {
case <-ch:
case <-requestCtx.Done():
	return
	}
}
а мы тут все равно обязаны вычитать канал до закрытия, но это один следующий раз, там скорее всего один элемент при закрытии канала,
т.к. при следующем Write сработает requestCtx.Close()
и потом сделается Close()
а что если вообще не вычитывать при requestCtx.Done?
будет ErrNoConsumer при следующем Write и потом Close()

тогда так:
- левая сторона обязана следить за requextCtx при Write
- роутер может следить за контекстом и завершаться, если закрылся
- роутер может не вычитывать канал до конца. В этом случае при следующем Write будет ErrNoConsumer

continuation:
роутер вызывает Done(requestCtx.Err())
под капотом неблокирующий взод
continuationChan chan error
при Done(err): если в continuationChan что-то есть, то сразу паника


стоп, слева вообще не должно быть default при Write. Надо просто следить за контекстом слева. Вот правая сторона обязана закрывать контекст


 */


func (rs *implResponseWriter) Write(obj any) error {
	sendTimeoutTimerChan := rs.tm.NewTimerChan(time.Duration(rs.sendTimeout))
	chunkImpl := newIChunk(rs.continuationCh)
	chunkImpl.(*chunk).obj = obj
	select {
	case rs.ch <- chunkImpl:
		<-
	case <-rs.clientCtx.Done():
	case <-sendTimeoutTimerChan:
		return ErrNoConsumer
	}
	return rs.clientCtx.Err()
}

func (rs *implResponseWriter) Close(err error) {
	*rs.resultErr = err
	close(rs.ch)
}

func (r *implIResponder) checkStarted() {
	if r.started {
		panic("unable to start the response more than once")
	}
	r.started = true
}

func (r ResponseMeta) Mode() RespondMode {
	return r.mode
}
