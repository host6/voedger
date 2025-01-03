/*
 * Copyright (c) 2024-present Sigma-Soft, Ltd.
 * @author: Nikolay Nikitin
 */

package appdef

import (
	"strings"

	"github.com/voedger/voedger/pkg/coreutils/utils"
)

// Returns all types that match the filter.
func FilterMatches(f IFilter, types SeqType) SeqType {
	return func(yield func(IType) bool) {
		for t := range types {
			if f.Match(t) {
				if !yield(t) {
					return
				}
			}
		}
	}
}

// Returns the first type that matches the filter.
// Returns nil if no types match the filter.
func FirstFilterMatch(f IFilter, types SeqType) IType {
	for t := range types {
		if f.Match(t) {
			return t
		}
	}
	return nil
}

func (k FilterKind) MarshalText() ([]byte, error) {
	var s string
	if k < FilterKind_count {
		s = k.String()
	} else {
		s = utils.UintToString(k)
	}
	return []byte(s), nil
}

// Renders an FilterKind in human-readable form, without "FilterKind_" prefix,
// suitable for debugging or error messages
func (k FilterKind) TrimString() string {
	const pref = "FilterKind_"
	return strings.TrimPrefix(k.String(), pref)
}
