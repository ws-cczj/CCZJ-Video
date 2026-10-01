// Package apperror 是全应用唯一的错误类型层。
//
// 为什么要带码：Wails 把 Go 的 error 直接转成字符串交给前端，界面拿到的就只剩这句话。
// 之前三套写法并存（fmt.Errorf 的散文、扩展包自己的 reason_code、proxy 里用
// strings.Contains 猜错误类型），调用方要么去匹配文案、要么什么都判不出来 ——
// 文案一改，重试逻辑和界面分组就静默失效。
//
// 约定（逐包替换时就按这三条，别再引入第四套）：
//   - 会跨出包边界的错误（绑定方法的返回值、给界面或上层看的错误）用这里的类型，
//     选一个 Code，Message 写给人看的那句话，底层原文挂 Cause。
//   - 只在包内流转的错误继续用 fmt.Errorf，但原因必须用 %w 带上，不许丢掉。
//   - 判断错误类型用 CodeOf / errors.As / errors.Is；禁止 strings.Contains(err.Error(), ...)。
//     要重试还是放弃，看 Code 或 Cause 的类型，不看文案。
package apperror

import (
	"errors"
	"fmt"
)

// Code 是稳定的错误类别。它的值就是界面和日志里看到的前缀，所以只能增改、不能改文案。
type Code string

const (
	// Validation：入参不合法，调用方改参数就能修，重试无意义。
	Validation Code = "VALIDATION"
	// Conflict：状态撞了（重名、已存在、正在跑），需要用户决定而不是重试。
	Conflict Code = "CONFLICT"
	// NotFound：要的东西不在库里、不在磁盘上或已被删除。
	NotFound Code = "NOT_FOUND"
	// Cancelled：调用方或应用主动收手，不是故障。
	Cancelled Code = "CANCELLED"
	// Timeout：底层在期限内没回来，同一次操作可以再试。
	Timeout Code = "TIMEOUT"
	// Unavailable：对端或本地服务不可达（网络、被限流、进程没起来）。
	Unavailable Code = "UNAVAILABLE"
	// Storage：该写的没写下去、该读的读不出来（磁盘、数据库、权限）。
	Storage Code = "STORAGE"
	// Corrupt：数据存在但坏了（校验不过、格式解不开、长度对不上）。
	Corrupt Code = "CORRUPT"
	// Unsupported：认不出的格式或能力，重试多少次都一样。
	Unsupported Code = "UNSUPPORTED"
	// Internal：走到不该走到的分支，属于代码问题。
	Internal Code = "INTERNAL"

	// DownloadDuplicate 是既有下载流程的界面契约：前端按这个码弹「是否覆盖」。
	DownloadDuplicate Code = "DOWNLOAD_DUPLICATE"
)

// Error 是带码错误。它同时是普通 Go error：Message 给人看，Cause 给日志和 errors.Is 用。
type Error struct {
	Code    Code
	Message string
	Cause   error
}

// Error 的渲染格式是对外契约的一部分：前端 normalizeApiError 按「CODE: 消息」拆码，
// 扩展包面板按「reason_code: 详情」显示。改这里要同时改前端。
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
func (e *Error) Unwrap() error { return e.Cause }

// New 造一个不带底层原因的带码错误。
func New(code Code, message string) error { return &Error{Code: code, Message: message} }

// Newf 造一个不带底层原因的带码错误，消息按 fmt 拼。
func Newf(code Code, format string, args ...any) error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Wrap 造一个带码错误并把底层原因挂上：日志里看得到原文，调用方按 Code 判类别。
func Wrap(code Code, cause error, message string) error {
	return &Error{Code: code, Message: message, Cause: cause}
}

// CodeOf 取错误的码；不是带码错误就返回空串（调用方据此退回按 Cause 判断）。
func CodeOf(err error) Code {
	var appError *Error
	if errors.As(err, &appError) {
		return appError.Code
	}
	return ""
}

// Is 判断错误的码是否等于给定码，省去调用方自己 errors.As。
func Is(err error, code Code) bool { return CodeOf(err) == code }
