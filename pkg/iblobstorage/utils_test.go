/*
 * Copyright (c) 2024-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package iblobstorage

import (
	"encoding/base64"
	"log"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewSUUID(t *testing.T) {
	suuid := NewSUUID()

	log.Println(suuid)

	const expectedLength = 43
	require.Len(t, suuid, expectedLength)

	require.Regexp(t, "[a-zA-Z0-9-_]", suuid)

	_, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(string(suuid))
	require.NoError(t, err)
}

func TestDurationSeconds(t *testing.T) {
	require := require.New(t)
	t.Run("1 day", func(t *testing.T) {
		require.Equal(86400, DurationType_1Day.Seconds())
	})
	t.Run("1 year", func(t *testing.T) {
		require.Equal(86400*365, DurationType_1Year.Seconds())
	})
	t.Run("2 days", func(t *testing.T) {
		require.Equal(86400*2, DurationType(2).Seconds())
	})
}
