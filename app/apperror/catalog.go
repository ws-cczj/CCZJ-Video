package apperror

import (
	"errors"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

// Definition is the stable public catalogue. Domain errors use semantic names;
// HTTP status and default display policy are separate from semantic codes.
type Definition struct {
	Message string
	Status  int
	Level   string
}

var definitions = map[Code]Definition{
	Validation:             {"请求参数不正确", http.StatusBadRequest, "warn"},
	Conflict:               {"当前操作存在冲突", http.StatusConflict, "warn"},
	NotFound:               {"请求的内容不存在", http.StatusNotFound, "warn"},
	Cancelled:              {"操作已取消", 499, "info"},
	Timeout:                {"操作超时", http.StatusGatewayTimeout, "warn"},
	Unavailable:            {"服务暂时不可用", http.StatusServiceUnavailable, "warn"},
	Storage:                {"数据保存或读取失败", http.StatusInternalServerError, "error"},
	Corrupt:                {"数据格式无法识别", http.StatusUnprocessableEntity, "error"},
	Unsupported:            {"当前操作暂不支持", http.StatusNotImplemented, "warn"},
	Internal:               {"操作失败，请查看日志", http.StatusInternalServerError, "error"},
	AuthenticationRequired: {"当前操作需要登录", http.StatusUnauthorized, "warn"},
	PermissionDenied:       {"当前操作被拒绝", http.StatusForbidden, "warn"},
	RateLimited:            {"请求过于频繁，请稍后重试", http.StatusTooManyRequests, "warn"},
	DownloadDuplicate:      {"下载文件已存在", http.StatusConflict, "warn"},
}

func Describe(code Code) Definition {
	if definition, ok := definitions[code]; ok {
		return definition
	}
	return definitions[Internal]
}

// Envelope is the stable transport contract. HTTP status belongs to the HTTP
// adapter; successful Wails values retain their existing return contracts.
type Envelope struct {
	Code              Code   `json:"code"`
	Message           string `json:"message"`
	RetryAfterSeconds int    `json:"retryAfterSeconds,omitempty"`
	Operation         string `json:"operation,omitempty"`
	RequestID         string `json:"requestId,omitempty"`
	Logged            bool   `json:"logged,omitempty"`
}

func Encode(err error) Envelope {
	p := Public(err)
	return Envelope{Code: p.Code, Message: p.Message, RetryAfterSeconds: p.RetryAfterSeconds, Operation: p.Operation, RequestID: p.RequestID, Logged: p.Logged}
}

type operationError struct {
	cause         error
	operation, id string
	logged        atomic.Bool
}

func (e *operationError) Error() string {
	return string(CodeOf(e.cause)) + ": " + Public(e.cause).Message
}
func (e *operationError) Unwrap() error   { return e.cause }
func (e *operationError) ErrorCode() Code { return CodeOf(e.cause) }
func (e *operationError) PublicError() Response {
	p := Public(e.cause)
	p.Operation = e.operation
	p.RequestID = e.id
	p.Logged = e.logged.Load()
	return p
}
func WithOperation(err error, operation, id string) error {
	if err == nil {
		return nil
	}
	return &operationError{cause: err, operation: operation, id: id}
}

// MarkLogged is called after a middleware has written the private diagnostic.
// Correlation metadata alone must never suppress the transport's fallback log.
func MarkLogged(err error) {
	var operation *operationError
	if errors.As(err, &operation) {
		operation.logged.Store(true)
	}
}

// Diagnostic retains the private cause when the Wails-facing wrapper renders
// only a public message (the runtime also serializes err.Error() separately).
func Diagnostic(err error) error {
	var operation *operationError
	if errors.As(err, &operation) {
		return operation.cause
	}
	return err
}

var requestSequence atomic.Uint64

func NewRequestID() string {
	return strconv.FormatInt(time.Now().UnixMilli(), 36) + "-" + strconv.FormatUint(requestSequence.Add(1), 36)
}
