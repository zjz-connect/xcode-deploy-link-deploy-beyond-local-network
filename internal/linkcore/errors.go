package linkcore

import "fmt"

type CodedError struct {
	Code    string
	Message string
	Cause   error
}

func (e *CodedError) Error() string {
	if e.Message != "" {
		return e.Code + ": " + e.Message
	}
	if e.Cause != nil {
		return e.Code + ": " + e.Cause.Error()
	}
	return e.Code
}

func (e *CodedError) Unwrap() error {
	return e.Cause
}

func coded(code string, message string, cause error) error {
	return &CodedError{Code: code, Message: message, Cause: cause}
}

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	var target *CodedError
	if asCodedError(err, &target) {
		return target.Code
	}
	return "internal_error"
}

func asCodedError(err error, target **CodedError) bool {
	for err != nil {
		if value, ok := err.(*CodedError); ok {
			*target = value
			return true
		}
		type unwrapper interface{ Unwrap() error }
		value, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = value.Unwrap()
	}
	return false
}

func publicError(err error) string {
	if err == nil {
		return ""
	}
	var target *CodedError
	if asCodedError(err, &target) {
		return target.Error()
	}
	return fmt.Sprintf("internal_error: %T", err)
}

func ErrorDetails(err error) (string, string) {
	return errorCode(err), publicError(err)
}
