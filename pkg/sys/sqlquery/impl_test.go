/*
 * Copyright (c) 2024-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package sqlquery

import (
	"testing"

	"github.com/blastrain/vitess-sqlparser/sqlparser"
	"github.com/stretchr/testify/require"
)

func TestParser(t *testing.T) {
	stmt, err := sqlparser.Parse("select * from w123.mytable;")
	require.NoError(t, err)
	_ = stmt
}
