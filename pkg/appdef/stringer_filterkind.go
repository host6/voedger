// Code generated by "stringer -type=FilterKind -output=stringer_filterkind.go"; DO NOT EDIT.

package appdef

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[FilterKind_null-0]
	_ = x[FilterKind_QNames-1]
	_ = x[FilterKind_Types-2]
	_ = x[FilterKind_Tags-3]
	_ = x[FilterKind_And-4]
	_ = x[FilterKind_Or-5]
	_ = x[FilterKind_Not-6]
	_ = x[FilterKind_count-7]
}

const _FilterKind_name = "FilterKind_nullFilterKind_QNamesFilterKind_TypesFilterKind_TagsFilterKind_AndFilterKind_OrFilterKind_NotFilterKind_count"

var _FilterKind_index = [...]uint8{0, 15, 32, 48, 63, 77, 90, 104, 120}

func (i FilterKind) String() string {
	if i >= FilterKind(len(_FilterKind_index)-1) {
		return "FilterKind(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _FilterKind_name[_FilterKind_index[i]:_FilterKind_index[i+1]]
}