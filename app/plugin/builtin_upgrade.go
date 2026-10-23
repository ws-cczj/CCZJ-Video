package plugin

import (
	"os"
	"path/filepath"

	"cczjVideo/app/apperror"
)

// Upgrade only verified shipped manifests and executable files. Modified
// packs and deliberately removed packs remain under the user's control.
func upgradeDiagnosticsManifest(root, id string) error {
	if id != "diagnostics-panel" {
		return nil
	}
	manifest := filepath.Join(root, id, manifestFileName)
	old, err := os.ReadFile(manifest)
	if err != nil {
		return nil
	}
	if _, ok := shippedBuiltinParts(id, filepath.Join(root, id)); !ok {
		return nil
	}
	newManifest, err := builtinFS.ReadFile(builtinSourceRoot + "/" + id + "/" + manifestFileName)
	if err != nil {
		return apperror.Wrap(apperror.Internal, err, "读取诊断包清单失败")
	}
	if string(old) == string(newManifest) {
		return nil
	}
	staged, err := os.CreateTemp(filepath.Dir(manifest), ".manifest-*")
	if err != nil {
		return apperror.Wrap(apperror.Storage, err, "暂存诊断包清单失败")
	}
	defer os.Remove(staged.Name())
	_, err = staged.Write(newManifest)
	closeErr := staged.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(staged.Name(), manifest)
	}
	if err != nil {
		return apperror.Wrap(apperror.Storage, err, "升级诊断包清单失败")
	}
	return nil
}
