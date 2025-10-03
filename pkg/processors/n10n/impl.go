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
	"time"

	"github.com/voedger/voedger/pkg/coreutils"
	"github.com/voedger/voedger/pkg/goutils/logger"
	"github.com/voedger/voedger/pkg/pipeline"
)

func getCreateChannelParams(ctx context.Context, work pipeline.IWorkpiece) (err error) {
	n10nWP := work.(*n10nWorkpiece)
	if err = json.Unmarshal([]byte(n10nWP.URLPayload()), &n10nWP.createChannelParams); err != nil {
		return fmt.Errorf("cannot unmarshal input payload %w", err)
	}
	return nil
}

func newChannel(ctx context.Context, work pipeline.IWorkpiece) (err error) {
	n10nWP := work.(*n10nWorkpiece)
	n10nWP.channelID, err = n10nWP.n10nBroker.NewChannel(n10nWP.createChannelParams.SubjectLogin, hours24)
	return err
}

func initResponse(ctx context.Context, work pipeline.IWorkpiece) (err error) {
	n10nWP := work.(*n10nWorkpiece)
	n10nWP.responseWriter = n10nWP.Responder().InitResponse(http.StatusOK) // actually does not metter bu need to match bus contract
	return nil

}

func sendChannelIDSSEEvent(ctx context.Context, work pipeline.IWorkpiece) (err error) {
	n10nWP := work.(*n10nWorkpiece)
	return n10nWP.responseWriter.Write(fmt.Sprintf("event: channelId\ndata: %s\n\n", n10nWP.channelID))
}

func subscribe(ctx context.Context, work pipeline.IWorkpiece) (err error) {
	n10nWP := work.(*n10nWorkpiece)
	for _, projectionKey := range n10nWP.createChannelParams.ProjectionKey {
		if err = n10nWP.n10nBroker.Subscribe(n10nWP.channelID, projectionKey); err != nil {
			return coreutils.NewHTTPErrorf(n10nErrorToStatusCode(err), "subscribe failed: %w", err)
		}
		n10nWP.subscribedProjectionKeys = append(n10nWP.subscribedProjectionKeys, projectionKey)
	}
	return nil
}

func (rs *responseSender) OnErr(err error, work interface{}, _ pipeline.IWorkpieceContext) (newErr error) {
	n10nWP := work.(*n10nWorkpiece)
	for _, subscribedKey := range n10nWP.subscribedProjectionKeys {
		if err = n10nWP.n10nBroker.Unsubscribe(n10nWP.channelID, subscribedKey); err != nil {
			logger.Error(fmt.Sprintf("failed to unsubscribe key %#v: %s", subscribedKey, err))
		}
	}
	return coreutils.WrapSysError(err, http.StatusBadRequest)
}

func (m *N10NMessage) ExpiresIn() time.Duration {
	return m.expiresIn
}

func (m *N10NMessage) Subscriptions() []Subscription {
	return m.subscriptions
}

func (m *N10NMessage) URLPayload() string {
	return m.urlPayload
}

func (m *n10nWorkpiece) Release() {}
