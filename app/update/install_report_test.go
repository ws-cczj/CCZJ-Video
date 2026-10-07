package update

import (
	"testing"

	"cczjVideo/app/updater"
)

// 回执分类是这条链上唯一一点判断：脚本写下什么，界面就得据此决定说不说话。
// 判反了，「没装上」会被当成成功而继续沉默——这正是测试机那台机器上的形状。
func TestInstallReportClassifiesVerdicts(t *testing.T) {
	cases := []struct {
		name    string
		verdict string
		failed  bool
	}{
		{"换成", "ok", false},
		{"退回旧版成功", "rollback_ok", false},
		{"文件仍被占用", "swap_failed", true},
		{"换完却找不到本体", "verify_failed", true},
		{"退回失败", "rollback_failed", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var service Service
			service.reportInstallVerdict(c.verdict)
			got := service.LastInstallReport()
			if got.Result != c.verdict {
				t.Fatalf("回执原话 = %q, 期望 %q", got.Result, c.verdict)
			}
			if got.Failed != c.failed {
				t.Fatalf("%q 的 Failed = %v, 期望 %v", c.verdict, got.Failed, c.failed)
			}
			// 报的是此刻在跑的版本，不是安装想达到的那个：用户要的是「我现在是谁」。
			if got.Running != updater.Version {
				t.Fatalf("Running = %q, 期望当前编译版本 %q", got.Running, updater.Version)
			}
		})
	}
}

// 没有回执时不该说任何话：首次安装、非 Windows 路径都是这个形状，
// 把它们说成「上次更新失败」就是凭空造一条用户从没经历过的故障。
func TestInstallReportEmptyUntilAVerdictArrives(t *testing.T) {
	var service Service
	got := service.LastInstallReport()
	if got.Result != "" || got.Failed {
		t.Fatalf("没有回执时的报告 = %+v, 期望全空", got)
	}
}
