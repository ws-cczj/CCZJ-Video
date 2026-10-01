/**
 * 内置扩展包：设置页的「日志」分组。
 *
 * 面板本体（frontend/src/components/LogPanel.vue）是编译在应用里的，这个包只负责把
 * 它挂到设置页的分组条上。于是「要不要这一项」变成了「这个文件夹在不在」：删掉
 * plugins/logs-panel 就是从设置页去掉日志，把文件夹放回来就又要回来了。
 *
 * 应用只在第一次启动时把这个包落盘，之后不再碰它——所以删掉不会在下次启动复活，
 * 改这里的代码也不会被版本更新覆盖。
 */
export function setup(cczj) {
  cczj.settingsTab({
    id: 'logs',
    // label 给函数：切换语言后这一行跟着重算，不会冻在登记那一刻的中文上。
    label: function () { return cczj.i18n.t('logs.title') },
    icon: 'terminal',
    order: 40,
    component: cczj.components.LogPanel,
  })
}
