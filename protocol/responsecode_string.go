
package protocol

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[CodeOK-0]
	_ = x[CodeBadRequest-1]
	_ = x[CodeNotFound-2]
	_ = x[CodeUnauthorized-3]
	_ = x[CodeServerError-4]
}

const _ResponseCode_name = "CodeOKCodeBadRequestCodeNotFoundCodeUnauthorizedCodeServerError"

var _ResponseCode_index = [...]uint8{0, 6, 20, 32, 48, 63}

func (i ResponseCode) String() string {
	if i >= ResponseCode(len(_ResponseCode_index)-1) {
		return "ResponseCode(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _ResponseCode_name[_ResponseCode_index[i]:_ResponseCode_index[i+1]]
}
