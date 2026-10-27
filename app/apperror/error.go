// Package apperror maps Go errors to the public application protocol.
// Domain packages may use their own errors and preserve causes with %w.
package apperror

import (
	"cczjVideo/app/applog"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
)

// Code identifies an outcome independently of its display message.
type Code string

const (
	// Validation：入参不合法，调用方改参数就能修，重试无意义。
	Validation Code = "VALIDATION"
	// Conflict：重名、已存在或操作中的状态冲突。
	Conflict Code = "CONFLICT"
	// NotFound：要的东西不在库里、不在磁盘上或已被删除。
	NotFound Code = "NOT_FOUND"
	// Cancelled：调用方或应用主动收手，不是故障。
	Cancelled Code = "CANCELLED"
	// Timeout：操作期限已过；是否重试由具体操作决定。
	Timeout Code = "TIMEOUT"
	// Unavailable：对端或本地服务不可达（网络、被限流、进程没起来）。
	Unavailable Code = "UNAVAILABLE"
	// Storage：该写的没写下去、该读的读不出来（磁盘、数据库、权限）。
	Storage Code = "STORAGE"
	// Corrupt：数据存在但坏了（校验不过、格式解不开、长度对不上）。
	Corrupt Code = "CORRUPT"
	// Unsupported：当前实现未支持的格式或能力。
	Unsupported Code = "UNSUPPORTED"
	// Internal：走到不该走到的分支，属于代码问题。
	Internal               Code = "INTERNAL"
	AuthenticationRequired Code = "AUTHENTICATION_REQUIRED"
	PermissionDenied       Code = "PERMISSION_DENIED"
	RateLimited            Code = "RATE_LIMITED"

	// DownloadDuplicate 是既有下载流程的界面契约：前端按这个码弹「是否覆盖」。
	DownloadDuplicate  Code = "DOWNLOAD_DUPLICATE"
	SourceURLDuplicate Code = "SOURCE_URL_DUPLICATE"
)

// Error 是带码错误。它同时是普通 Go error：Message 给人看，Cause 给日志和 errors.Is 用。
type Error struct {
	Code              Code
	Message           string
	Cause             error
	RetryAfterSeconds int
}

// Error renders diagnostic text. Older clients can still read its code prefix;
// current clients use the structured response produced by Marshal.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	message := e.Message
	switch {
	case e.Cause == nil:
	case message == "":
		message = e.Cause.Error()
	default:
		message += ": " + e.Cause.Error()
	}
	if message == "" {
		return string(e.Code)
	}
	if e.Code == "" {
		return message
	}
	return fmt.Sprintf("%s: %s", e.Code, message)
}

// Unwrap 让 Cause 能被 errors.Is / errors.As 看到，标准库的错误（sql.ErrNoRows、
// context.Canceled、*httperror 之类）不用复制一份到这套类型里。
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// ErrorCode also lets domain error types expose a public code without embedding Error.
func (e *Error) ErrorCode() Code {
	if e == nil {
		return ""
	}
	return e.Code
}

// New 造一个不带底层原因的带码错误。
func New(code Code, message string) error { return &Error{Code: code, Message: message} }

// Newf 造一个不带底层原因的带码错误，消息按 fmt 拼。
func Newf(code Code, format string, args ...any) error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Wrap 造一个带码错误并把底层原因挂上：日志里看得到原文，调用方按 Code 判类别。
func Wrap(code Code, cause error, message string) error {
	if cause == nil {
		return nil
	}
	if existing := CodeOf(cause); existing != "" {
		code = existing
	}
	return &Error{Code: code, Message: message, Cause: cause}
}

// CodeOf reads a domain code or classifies common standard-library errors.
func CodeOf(err error) Code {
	var coded interface{ ErrorCode() Code }
	if errors.As(err, &coded) {
		return coded.ErrorCode()
	}
	if errors.Is(err, context.Canceled) {
		return Cancelled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Timeout
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return Timeout
	}
	if errors.Is(err, sql.ErrNoRows) {
		return NotFound
	}
	return ""
}

// Is 判断错误的码是否等于给定码，省去调用方自己 errors.As。
func Is(err error, code Code) bool { return CodeOf(err) == code }

// Response is the public error value carried in Wails RuntimeError.cause.
// Diagnostic causes stay in Go's error chain and are not serialized to the UI.
type Response struct {
	Logged            bool   `json:"logged,omitempty"`
	Operation         string `json:"operation,omitempty"`
	RequestID         string `json:"requestId,omitempty"`
	Code              Code   `json:"code"`
	Message           string `json:"message"`
	RetryAfterSeconds int    `json:"retryAfterSeconds,omitempty"`
}

// Public supports independently defined domain errors through PublicError().
func Public(err error) Response {
	var public interface{ PublicError() Response }
	if errors.As(err, &public) {
		return public.PublicError()
	}
	code := CodeOf(err)
	if code == "" {
		code = Internal
	}
	result := Response{Code: code, Message: Describe(code).Message}
	var appError *Error
	if errors.As(err, &appError) && appError != nil && appError.Message != "" {
		result.Message = appError.Message
	}
	if code == Cancelled {
		result.Message = "操作已取消"
	}
	if code == Timeout {
		result.Message = "操作超时"
	}
	// A contextual wrapper must not discard a lower layer's retry metadata.
	for current := err; current != nil; current = errors.Unwrap(current) {
		if e, ok := current.(*Error); ok && e != nil && e.RetryAfterSeconds > 0 {
			result.RetryAfterSeconds = e.RetryAfterSeconds
			break
		}
	}
	return result
}

// Marshal is installed once at the Wails application boundary.
func Marshal(err error) []byte {
	envelope := Encode(err)
	if envelope.Code != Cancelled {
		// Operations already carry a diagnostic correlation record. Other bindings
		// still pass this single transport boundary and get a private native log.
		if !envelope.Logged {
			if envelope.RequestID == "" {
				envelope.RequestID = NewRequestID()
			}
			fields := applog.Fields{"code": envelope.Code, "request_id": envelope.RequestID, "error": Diagnostic(err)}
			if Describe(envelope.Code).Level == "error" {
				applog.ErrorFields("binding failed", fields)
			} else {
				applog.WarnFields("binding failed", fields)
			}
		}
		envelope.Logged = true
	}
	value, _ := json.Marshal(envelope)
	return value
}
