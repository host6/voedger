/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */
package storage

import "errors"

var (
	ErrKeyEmpty     = errors.New("key is empty")
	ErrKeyTooLong   = errors.New("key exceeds maximum length")
	ErrValueTooLong = errors.New("value exceeds maximum length")
	ErrInvalidTTL   = errors.New("TTL must be between 1 and 31536000 seconds")
)

