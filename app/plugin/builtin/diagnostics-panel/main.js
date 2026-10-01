/**
 * 内置扩展包：设置页的「诊断」分组。
 *
 * 和 logs-panel 同一套办法：面板本体在应用里，这个包只负责挂上分组条。
 * 删掉 plugins/diagnostics-panel 就是从设置页去掉诊断这一项。
 */
export function setup(cczj) {
  cczj.settingsTab({
    id: 'diagnostics',
    label: function () { return cczj.i18n.t('settings.diagnostics') },
    icon: 'monitor',
    order: 50,
    component: cczj.components.DiagnosticsPanel,
  })
}
