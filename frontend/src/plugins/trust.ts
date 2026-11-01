import { CCZJ } from '../constants/cczj'
import { GetSetting, SetSetting } from '../api/app'
import { useConfirmStore } from '../stores/confirm'
import { tr } from '../locales'
import { createScriptTrust } from './scriptTrust'

export const scriptTrust = createScriptTrust({
  load: () => GetSetting(CCZJ.Settings.pluginScriptTrust),
  save: value => SetSetting(CCZJ.Settings.pluginScriptTrust, value).then(() => {}),
  ask: (_id, name) => useConfirmStore().confirm({
    title: tr('permissions.scriptTrustTitle'),
    message: tr('permissions.scriptTrustPrompt', { name }),
    okText: tr('permissions.scriptTrustAllow'),
    cancelText: tr('permissions.scriptTrustDeny'),
    level: 'danger',
  }),
})
