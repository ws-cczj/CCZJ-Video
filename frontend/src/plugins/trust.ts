import { GetSetting, SetSetting } from '../api/app'
import { useConfirmStore } from '../stores/confirm'
import { tr } from '../locales'
import { createScriptTrust } from './scriptTrust'

export const scriptTrust = createScriptTrust({
  load: () => GetSetting('plugin_script_trust'),
  save: value => SetSetting('plugin_script_trust', value).then(() => {}),
  ask: (_id, name) => useConfirmStore().confirm({
    title: tr('permissions.scriptTrustTitle'),
    message: tr('permissions.scriptTrustPrompt', { name }),
    okText: tr('permissions.scriptTrustAllow'),
    cancelText: tr('permissions.scriptTrustDeny'),
    level: 'danger',
  }),
})
