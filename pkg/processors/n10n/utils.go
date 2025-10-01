/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package n10n

import (
	"errors"
	"net/http"

	"github.com/voedger/voedger/pkg/in10n"
	"github.com/voedger/voedger/pkg/in10nmem"
)

func n10nErrorToStatusCode(err error) int {
	switch {
	case errors.Is(err, in10n.ErrChannelDoesNotExist), errors.Is(err, in10nmem.ErrMetricDoesNotExists):
		return http.StatusBadRequest
	case errors.Is(err, in10n.ErrQuotaExceeded_Subscriptions), errors.Is(err, in10n.ErrQuotaExceeded_SubscriptionsPerSubject),
		errors.Is(err, in10n.ErrQuotaExceeded_Channels), errors.Is(err, in10n.ErrQuotaExceeded_ChannelsPerSubject):
		return http.StatusTooManyRequests
	}
	return http.StatusInternalServerError
}
