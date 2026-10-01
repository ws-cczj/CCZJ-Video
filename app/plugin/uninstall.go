package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
)

// Uninstall 删掉一个扩展包在磁盘上的目录——界面上那颗「卸载」按钮就是它。
//
// 这是本包里唯一会删用户文件的动作，所以每一道闸都收紧，任何一道不过就什么都删：
//  1. id 必须能在**当前注册表**里找着，目录由扫描给出，不拿界面传来的字符串拼路径；
//  2. 那个目录必须正好是 plugins 的一层子目录（防止注册表里混进一个被改坏的路径）；
//  3. 里面必须有 plugin.json：我们只删「是一个扩展包」的目录；
//  4. 应用自带的包（app/plugin/builtin/ 那批）拒绝：删别人的包是卸载，删应用自己的
//     东西不是，那一行在界面上压根不该有按钮，走到这里说明是别处传来的调用。
//
// 删完把启用状态里那一行也抹掉、并从注册表缓存里摘掉：重装同一个 id 时应该从「启用」
// 开始，而不是继承上次卸载前的开关；缓存不清的话界面刷新出来还是一行幽灵。
func (s *Service) Uninstall(id string) error {
	if strings.TrimSpace(id) == "" {
		return apperror.New(apperror.Validation, "扩展包 id 为空")
	}
	if isBuiltinPackID(id) {
		return apperror.Newf(apperror.Conflict, "%q 是应用自带的扩展包，不能从应用里卸载（想让它不出现请关掉开关）", id)
	}
	dir, err := s.packDir(id)
	if err != nil {
		return err
	}
	if isBuiltinPackID(filepath.Base(dir)) {
		return apperror.Newf(apperror.Conflict, "%q 是应用自带的扩展包，不能从应用里卸载（想让它不出现请关掉开关）", id)
	}
	root := s.Directory()
	if filepath.Dir(dir) != root {
		return apperror.Newf(apperror.Internal, "%q 的目录 %q 不在扩展包根目录下，没有删任何东西", id, dir)
	}
	if _, err := os.Stat(filepath.Join(dir, manifestFileName)); err != nil {
		return apperror.Newf(apperror.NotFound, "%q 里没有 %s，它不是扩展包目录，没有删任何东西", id, manifestFileName)
	}
	if err := os.RemoveAll(dir); err != nil {
		return apperror.Wrap(apperror.Storage, err, fmt.Sprintf("删除扩展包目录 %q 失败", dir))
	}

	states := s.states()
	if _, recorded := states[id]; recorded {
		delete(states, id)
		if err := s.saveStates(states); err != nil {
			// 目录已经删掉了，状态没清不算失败——那一条下次扫描也用不上。留痕即可。
			applog.Warn("卸载扩展包 %q 后清理启用状态失败: %v", id, err)
		}
	}
	s.forget(id)
	applog.InfoFields("扩展包已卸载", applog.Fields{"id": id, "dir": dir})
	return nil
}

// forget 把某个包从注册表缓存里摘掉（卸载之后不重扫也不该在界面上留一行）。
func (s *Service) forget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.scanned {
		return
	}
	kept := make([]Info, 0, len(s.infos))
	for _, info := range s.infos {
		if info.ID == id {
			continue
		}
		kept = append(kept, info)
	}
	s.infos = kept
}
