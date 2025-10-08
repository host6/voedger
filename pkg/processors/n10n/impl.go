/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package n10n

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/voedger/voedger/pkg/bus"
	"github.com/voedger/voedger/pkg/coreutils"
	"github.com/voedger/voedger/pkg/goutils/logger"
	"github.com/voedger/voedger/pkg/in10n"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/pipeline"
)

func (m *implIN10NProc) Handle(requestCtx context.Context, body []byte, responder bus.IResponder) error {
	wp := &n10nWorkpiece{
		body:       body,
		requestCtx: requestCtx,
		responder:  responder,
		n10nBroker: m.n10nBroker,
	}
	err := m.pipeline.SendAsync(wp)
	<-wp.doneCh
	return err
}

func getCreateChannelParams(ctx context.Context, work pipeline.IWorkpiece) (outWork pipeline.IWorkpiece, err error) {
	n10nWP := work.(*n10nWorkpiece)
	if err = json.Unmarshal(n10nWP.body, &n10nWP.createChannelParams); err != nil {
		return work, fmt.Errorf("cannot unmarshal input payload %w", err)
	}
	return work, nil
}

func newChannel(ctx context.Context, work pipeline.IWorkpiece) (outWork pipeline.IWorkpiece, err error) {
	n10nWP := work.(*n10nWorkpiece)
	n10nWP.channelID, err = n10nWP.n10nBroker.NewChannel(n10nWP.createChannelParams.SubjectLogin, hours24)
	return work, err
}

func initResponse(ctx context.Context, work pipeline.IWorkpiece) (outWork pipeline.IWorkpiece, err error) {
	n10nWP := work.(*n10nWorkpiece)
	n10nWP.responseWriter = n10nWP.responder.InitResponse(http.StatusOK)
	return work, nil
}

func sendChannelIDSSEEvent(ctx context.Context, work pipeline.IWorkpiece) (outWork pipeline.IWorkpiece, err error) {
	n10nWP := work.(*n10nWorkpiece)
	return work, n10nWP.responseWriter.Write(
		fmt.Sprintf("event: channelId\ndata: %s\n\n", n10nWP.channelID),
	)
}

func subscribe(ctx context.Context, work pipeline.IWorkpiece) (outWork pipeline.IWorkpiece, err error) {
	n10nWP := work.(*n10nWorkpiece)
	for _, projectionKey := range n10nWP.createChannelParams.ProjectionKey {
		if err = n10nWP.n10nBroker.Subscribe(n10nWP.channelID, projectionKey); err != nil {
			return work, coreutils.NewHTTPErrorf(n10nErrorToStatusCode(err), "subscribe failed: %w", err)
		}
		n10nWP.subscribedProjectionKeys = append(n10nWP.subscribedProjectionKeys, projectionKey)
	}
	return work, nil
}

func watchChannel(ctx context.Context, work pipeline.IWorkpiece) (outWork pipeline.IWorkpiece, err error) {
	n10nWP := work.(*n10nWorkpiece)
	// RequestCtx tracks both http request and VVM contexts
	n10nWP.n10nBroker.WatchChannel(n10nWP.requestCtx, n10nWP.channelID, func(projection in10n.ProjectionKey, offset istructs.Offset) {
		sseMessage := fmt.Sprintf("event: %s\ndata: %d\n\n", projection.ToJSON(), offset)
		if err := n10nWP.responseWriter.Write(sseMessage); err != nil {
			// could happen if router stopped to listen for bus
			// more likely request ctx is closed
			// WatchChannel will exit in this case
			logger.Error("failed to send sse message:", sseMessage)
		}
	})
	return work, nil
}

func (rs *finishResponse) DoAsync(_ context.Context, work pipeline.IWorkpiece) (outWork pipeline.IWorkpiece, err error) {
	n10nWP := work.(*n10nWorkpiece)
	n10nWP.responseWriter.Close(n10nWP.resultErr)
	close(n10nWP.doneCh)
	return work, nil
}

func (rs *finishResponse) OnError(_ context.Context, err error) {
	// n10nWP := work.(*n10nWorkpiece)
	// for _, subscribedKey := range n10nWP.subscribedProjectionKeys {
	// 	if err = n10nWP.n10nBroker.Unsubscribe(n10nWP.channelID, subscribedKey); err != nil {
	// 		logger.Error(fmt.Sprintf("failed to unsubscribe key %#v: %s", subscribedKey, err))
	// 	}
	// }
	// n10nWP.resultErr = coreutils.WrapSysError(err, http.StatusBadRequest)
}

func (rs *finishResponse) Flush(callback pipeline.OpFuncFlush) (err error) {
	logger.Info("")
	return nil
}

func (m *n10nWorkpiece) Release() {}
