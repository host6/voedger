/*
 * Copyright (c) 2021-present Sigma-Soft, Ltd.
 * @author: Nikolay Nikitin
 */

package appdef

// Operation document.
type IODoc interface {
	IDoc

	// Unwanted type assertion stub
	isODoc()
}

type IODocBuilder interface {
	IODoc
	IDocBuilder
}

// Operation document record.
type IORecord interface {
	IContainedRecord

	// Unwanted type assertion stub
	isORecord()
}

type IORecordBuilder interface {
	IORecord
	IContainedRecordBuilder
}