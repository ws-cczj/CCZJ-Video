/**
 * 内置扩展包：设置页的「动画」分组 + 这套动效的参数层。
 *
 * 面板本体（frontend/src/components/MotionPanel.vue）和「关闭动画」的退化规则都编译在
 * 应用里，这个包带的是两样东西：把分组挂上设置页，以及 motion.css 里那组时长与缓动。
 * 停用或删掉这个包，分组没了、参数层没了，界面退回应用自带的基线节奏——动画本身照旧，
 * 设置页的开关也还在（它读的是应用基线，不该因为包不见了就把自己锁在关闭状态）。
 */
export function setup(cczj) {
  cczj.settingsTab({
    id: 'motion',
    // label 给函数：切换语言后这一行跟着重算，不会冻在登记那一刻的中文上。
    label: function () { return cczj.i18n.t('motion.title') },
    icon: 'sparkle',
    // 内置分组占了 10/20/30 与 60/70，日志 40、诊断 50；动画挨着主题放。
    order: 25,
    component: cczj.components.MotionPanel,
  })
}
