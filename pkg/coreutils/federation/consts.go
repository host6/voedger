/*
 * Copyright (c) 2024-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package federation

import (
	"regexp"

	"github.com/voedger/voedger/pkg/iblobstorage"
)

var (
	TemporaryBLOB_URLTTLToDurationLs = map[string]iblobstorage.DurationType{
		"1d": iblobstorage.DurationType_1Day,
		"1y": iblobstorage.DurationType_1Year,
	}
	TemporaryBLOBDurationToURLTTL = map[iblobstorage.DurationType]string{
		iblobstorage.DurationType_1Day:  "1d",
		iblobstorage.DurationType_1Year: "1y",
	}
	blobCreatePersistentRespRE = regexp.MustCompile(`"blobID":\s*(\d+)`)
	blobCreateTempRespRE       = regexp.MustCompile(`"blobSUUID":\s*"(.+)"`)
)
