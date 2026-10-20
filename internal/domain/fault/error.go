package fault

// Error 保留 API 的具体错误边界，同时仍是标准 error。
type Error struct {
	Code    string
	Message string
	cause   error
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func New(code string, message string) error {
	return &Error{Code: code, Message: message}
}

// Wrap 保留定位用的原因；Error() 只输出可对外提供的安全消息。
func Wrap(code, message string, cause error) error {
	return &Error{Code: code, Message: message, cause: cause}
}

func (e *Error) Unwrap() error { return e.cause }
