package plugin

import (
	"errors"
	"fmt"
	"strings"

	"cczjVideo/app/apperror"
)

// 原因码与 docs/plugins.md §2 一一对应：界面按 reason_code 分组翻译，
// reason 是可贴给用户看的人话（里面保留解码器的原文，便于作者定位写错的键名）。
const (
	reasonManifestTooLarge   = "manifest_too_large"
	reasonManifestUnreadable = "manifest_unreadable"
	reasonJSONInvalid        = "json_invalid"
	reasonUnknownField       = "unknown_field"
	reasonManifestVersion    = "manifest_version"
	reasonIDInvalid          = "id_invalid"
	reasonIDDirMismatch      = "id_dir_mismatch"
	reasonIDDuplicate        = "id_duplicate"
	reasonVersionInvalid     = "version_invalid"
	reasonNameMissing        = "name_missing"
	reasonKindInvalid        = "kind_invalid"
	reasonKindSection        = "kind_section"
	reasonPermissionInvalid  = "permission_invalid"
	reasonSectionMissing     = "section_missing"
	reasonPathInvalid        = "path_invalid"
	reasonPathMissing        = "path_missing"
	reasonPathTooLarge       = "path_too_large"
	reasonEntryEmpty         = "entry_empty"
	reasonScaleInvalid       = "scale_invalid"
	reasonResolutionChanging = "resolution_changing_pass"
	reasonLimitExceeded      = "limit_exceeded"
	reasonStrategyInvalid    = "strategy_invalid"
	reasonShaderInvalid      = "shader_invalid"
	reasonThemeInvalid       = "theme_invalid"
	reasonScriptInvalid      = "script_invalid"
)

// packError 承载「整包判 invalid」的原因，类型就是全应用共用的 apperror.Error：
// 校验失败的包只会走到这里一次，成功路径上不分配错误值。
//
// 码保持小写 snake_case —— docs/plugins.md §2 那张表就是码表本身，界面按 reason_code
// 分组、拖拽安装把码原样显示，改大小写会同时改掉文档和界面。变的只有类型：全应用
// 一套带码错误，不再各写一套。
type packError = apperror.Error

// 码在本包里保持普通 string：Info.ReasonCode 下发的就是字符串，散落的判定代码也按
// 字符串比较，套一层命名类型只会多出 conversions 而没有额外约束。
func newPackError(code string, format string, args ...any) *packError {
	return &apperror.Error{Code: apperror.Code(code), Message: fmt.Sprintf(format, args...)}
}

// inSection 在错误消息前面加上「出错的是哪一段」，原因码保持不变——
// unknown_field 这类码必须一路传到界面，前端才认得出该高亮哪个键。
func inSection(err error, section string) error {
	var pe *packError
	if !asPackError(err, &pe) {
		return err
	}
	return newPackError(string(pe.Code), "%s: %s", section, pe.Message)
}

func asPackError(err error, target **packError) bool { return errors.As(err, target) }

// packErrorMessage 给日志用：只有原因码太干，消息里带上出错路径才排得查明。
func packErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}
