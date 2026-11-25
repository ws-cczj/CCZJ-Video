<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../components/Icon.vue'
import { Button } from '../../components/ui'
import { useViewActivity } from '../../composables/useViewActivity'
import { useMotionStore } from '../../stores/motion'
import DanmakuLikeFeedback from './DanmakuLikeFeedback.vue'

const { t } = useI18n()
const motion = useMotionStore()
const active = useViewActivity(undefined, 'committed')
const supportsTravel = typeof CSS !== 'undefined' && CSS.supports('width', '1cqw')
const animated = computed(() => supportsTravel && motion.loaded && motion.allows('feedback'))
const paused = ref(false)
const announcement = ref('')
const lanes = reactive([
  { messages: ['incoming', 'rewatch', 'greatScene'], index: 0, likes: 0, feedback: false },
  { messages: ['hello', 'favorite', 'keepWatching'], index: 0, likes: 0, feedback: false },
])
type Lane = typeof lanes[number]

function message(lane: Lane): string {
  return t(`danmakuSources.previewMessages.${lane.messages[lane.index]}`)
}

function advance(lane: Lane): void {
  lane.index = (lane.index + 1) % lane.messages.length
  lane.likes = 0
  lane.feedback = false
}

function like(lane: Lane): void {
  lane.likes++
  lane.feedback = true
  announcement.value = t('danmakuSources.previewLikeStatus', { text: message(lane), count: lane.likes })
}
</script>

<template>
  <div class="danmaku-preview" role="group" :aria-label="t('danmakuSources.previewLabel')">
    <div class="danmaku-preview-stage" :class="{ 'is-animated': animated, 'is-paused': paused || !active }">
      <div v-for="(lane, index) in lanes" :key="index" class="danmaku-preview-lane">
        <Button type="button" size="sm" variant="ghost" class="danmaku-preview-comment"
          :ariaLabel="t('danmakuSources.previewLike', { text: message(lane) })"
          @click="like(lane)" @animationiteration.self="advance(lane)">
          {{ message(lane) }}
          <DanmakuLikeFeedback v-if="lane.feedback" :key="lane.likes" class="danmaku-preview-like"
            @finished="lane.feedback = false" />
        </Button>
      </div>
    </div>
    <Button v-if="animated" type="button" size="sm" variant="ghost" icon class="danmaku-preview-play"
      :ariaLabel="t(paused ? 'danmakuSources.previewResume' : 'danmakuSources.previewPause')"
      :title="t(paused ? 'danmakuSources.previewResume' : 'danmakuSources.previewPause')" @click="paused = !paused">
      <Icon :name="paused ? 'play' : 'pause'" :size="16" aria-hidden="true" />
    </Button>
    <span class="cczj-sr-only" role="status">{{ announcement }}</span>
  </div>
</template>

<style scoped>
.danmaku-preview {
  @apply cczj-grid cczj-items-center cczj-gap-3 cczj-min-w-0;
  --preview-comment-height: 28px;
  grid-column: 1 / -1;
  grid-template-columns: minmax(0, 1fr) auto;
}

.danmaku-preview-stage {
  @apply cczj-overflow-hidden cczj-min-w-0;
  container-type: inline-size;
  padding-block: 12px 3px;
}
.danmaku-preview-stage.is-animated {
  --preview-edge-mask: linear-gradient(to right, transparent, #000 16px, #000 calc(100% - 16px), transparent);
  -webkit-mask-image: var(--preview-edge-mask);
  mask-image: var(--preview-edge-mask);
}

.danmaku-preview-lane {
  @apply cczj-relative;
  min-height: calc(var(--preview-comment-height) + 8px);
  padding-inline: 4px;
}

.danmaku-preview-comment.ui-btn {
  @apply cczj-relative cczj-rounded-full cczj-text-primary cczj-whitespace-nowrap;
  --cczj-control-height: var(--preview-comment-height);
  min-height: var(--preview-comment-height);
  width: max-content;
  background: color-mix(in srgb, var(--bg-card) 76%, var(--bg-secondary));
  border-color: var(--border);
}

.danmaku-preview-lane:nth-child(2) .danmaku-preview-comment {
  @apply cczj-text-accent-readable;
}

.is-animated .danmaku-preview-comment {
  @apply cczj-absolute;
  inset: 0 auto auto 100%;
  animation: danmaku-travel var(--danmaku-travel-duration, 9s) linear infinite;
}

.is-animated .danmaku-preview-lane:nth-child(2) .danmaku-preview-comment { animation-delay: 1.6s; }
.is-paused .danmaku-preview-comment,
.is-animated .danmaku-preview-comment:active { animation-play-state: paused; }

@media (hover: hover) and (pointer: fine) {
  .is-animated .danmaku-preview-comment:hover { animation-play-state: paused; }
}

.is-animated .danmaku-preview-comment:focus-visible {
  /* Keep keyboard focus visible even when the current item is outside the lane. */
  left: 50%;
  transform: translateX(-50%) !important;
  animation-play-state: paused;
}

.danmaku-preview-play.ui-btn {
  min-width: 44px;
  min-height: 44px;
}

.danmaku-preview-like {
  @apply cczj-absolute;
  inset: -8px 3px auto auto;
}

@keyframes danmaku-travel {
  from { transform: translateX(0); }
  to { transform: translateX(calc(-100cqw - 100% - 8px)); }
}


@media (max-width: 600px) {
  .danmaku-preview { --danmaku-travel-duration: 6.5s; }
}

@media (pointer: coarse) {
  .danmaku-preview { --preview-comment-height: 44px; }
}
</style>
