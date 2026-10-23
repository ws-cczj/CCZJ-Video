<script setup lang="ts">
import { watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../components/Icon.vue'
import PlayerDialogFrame from './PlayerDialogFrame.vue'
import { usePlayerShortcuts } from '../usePlayerShortcuts'
const props=defineProps<{open:boolean}>()
const emit=defineEmits<{close:[]}>()
const { t }=useI18n()
interface ShortcutAction { id: string; labelKey: string; descriptionKey: string; defaultKeys: string[] }
const SHORTCUT_ACTIONS: ShortcutAction[] = [
  { id: 'togglePlay', labelKey: 'player.scTogglePlay', descriptionKey: 'player.scTogglePlayDesc', defaultKeys: ['Space', 'K'] },
  { id: 'seekBack', labelKey: 'player.scSeekBack', descriptionKey: 'player.scSeekBackDesc', defaultKeys: ['ArrowLeft'] },
  { id: 'seekForward', labelKey: 'player.scSeekForward', descriptionKey: 'player.scSeekForwardDesc', defaultKeys: ['ArrowRight'] },
  { id: 'seekBackBig', labelKey: 'player.scSeekBackBig', descriptionKey: 'player.scSeekBackBigDesc', defaultKeys: ['J'] },
  { id: 'seekForwardBig', labelKey: 'player.scSeekForwardBig', descriptionKey: 'player.scSeekForwardBigDesc', defaultKeys: ['L'] },
  { id: 'volumeUp', labelKey: 'player.scVolumeUp', descriptionKey: 'player.scVolumeUpDesc', defaultKeys: ['ArrowUp'] },
  { id: 'volumeDown', labelKey: 'player.scVolumeDown', descriptionKey: 'player.scVolumeDownDesc', defaultKeys: ['ArrowDown'] },
  { id: 'mute', labelKey: 'player.mute', descriptionKey: 'player.scMuteDesc', defaultKeys: ['M'] },
  { id: 'speedUp', labelKey: 'player.scSpeedUp', descriptionKey: 'player.scSpeedUpDesc', defaultKeys: [']'] },
  { id: 'speedDown', labelKey: 'player.scSpeedDown', descriptionKey: 'player.scSpeedDownDesc', defaultKeys: ['['] },
  { id: 'fullscreen', labelKey: 'player.fullscreen', descriptionKey: 'player.scFullscreenDesc', defaultKeys: ['F'] },
  { id: 'prevEp', labelKey: 'player.prev', descriptionKey: 'player.scPrevEpDesc', defaultKeys: ['P'] },
  { id: 'nextEp', labelKey: 'player.next', descriptionKey: 'player.scNextEpDesc', defaultKeys: ['N'] },
  { id: 'pip', labelKey: 'player.pip', descriptionKey: 'player.scPipDesc', defaultKeys: ['I'] },
]
const {
  shortcutMap,
  editingShortcutId,
  load: loadShortcuts,
  startEditShortcut,
  cancelEditShortcut,
  onShortcutKeyDown,
  resetShortcut,
  resetAllShortcuts,
  fmtKey,
} = usePlayerShortcuts(SHORTCUT_ACTIONS)

// 初始化快捷键
loadShortcuts()


watch(()=>props.open,open=>{if(open)loadShortcuts();else cancelEditShortcut()})
</script>
<template>
  <PlayerDialogFrame :open="open" :title="t('player.shortcutSettings')" variant="shortcut" @close="emit('close')" @keydown="onShortcutKeyDown">
          <div class="vp-modal-body vp-modal-scroll">
            <p class="vp-modal-hint">{{ t('player.shortcutHint') }}</p>
            <div class="vp-shortcut-list">
              <div v-for="action in SHORTCUT_ACTIONS" :key="action.id" class="vp-shortcut-row">
                <span class="vp-sc-label">{{ t(action.labelKey) }}</span>
                <span class="vp-sc-desc">{{ t(action.descriptionKey) }}</span>
                <div class="vp-sc-actions">
                  <button class="vp-sc-btn" :class="{ editing: editingShortcutId === action.id }"
                    @click.stop="startEditShortcut(action.id)">
                    <template v-if="editingShortcutId === action.id">
                      <span class="vp-sc-recording">{{ t('player.pressNewKey') }}</span>
                    </template>
                    <template v-else>
                      <span v-for="(k, ki) in (shortcutMap[action.id] || [])" :key="ki" class="vp-sc-key">{{ fmtKey(k)
                      }}</span>
                      <span v-if="!shortcutMap[action.id] || shortcutMap[action.id].length === 0"
                        class="vp-sc-none">{{ t('player.notSet') }}</span>
                    </template>
                  </button>
                  <button class="vp-sc-reset" @click.stop="resetShortcut(action.id)" :aria-label="t('player.restoreDefault')" :title="t('player.restoreDefault')">
                    <Icon name="reset" :size="12" />
                  </button>
                </div>
              </div>
            </div>
          </div>
          <div class="vp-modal-footer">
            <button class="vp-btn-secondary" @click="resetAllShortcuts">{{ t('player.resetAll') }}</button>
            <button class="vp-btn-primary" @click="emit('close')">{{ t('common.done') }}</button>
          </div>

  </PlayerDialogFrame>
</template>
<style scoped src="../../styles/components/player-shortcuts.css"></style>
