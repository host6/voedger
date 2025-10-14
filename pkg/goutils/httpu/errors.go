/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package httpu

import (
	"errors"
)

var (
	ErrUnexpectedStatusCode = errors.New("unexpected status code")
	ErrRetryableStatusCode  = errors.New("retryable status code")
)
