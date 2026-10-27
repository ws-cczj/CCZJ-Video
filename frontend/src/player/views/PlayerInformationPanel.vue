<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Window } from '../../api/runtime'
import { presentationProfile } from '../../platform/presentation'
import Icon from '../../components/Icon.vue'
import PlaybackRouteList from './PlaybackRouteList.vue'
import EpisodeLabel from './EpisodeLabel.vue'
import { hasDetailedEpisodeLabels } from '../videoInfoFormat'
import DoubanComments from '../../components/DoubanComments.vue'
import { Button } from '../../components/ui'
import type { PlayerPanelModel } from '../page/panelModel'

const props = defineProps<{model:PlayerPanelModel;collapsed:boolean}>()
const emit = defineEmits<{source:[key:string,line?:number];episode:[index:number];sort:[];close:[]}>()
const { t } = useI18n()
const mobile = presentationProfile().ui === 'mobile'
const details = props.model.details, catalog = props.model.catalog, episodeList = props.model.episodes
const detailedEpisodeLabels = computed(() => hasDetailedEpisodeLabels(episodeList.items))
const mobilePlayerTab = ref<'overview'|'comments'>('overview')
const mobileCommentsVisited = ref(false)
const mobileCommentsId = computed(() => {
  const id = String(details.video?.vod_douban_id || '').trim()
  return /^\d+$/.test(id) && Number(id)>0 ? id : ''
})
function showMobileComments():void { mobileCommentsVisited.value=true;mobilePlayerTab.value='comments' }
function onMinimizeApp():void { try { Window.Minimise() } catch { /* Host owns availability. */ } }
</script>
<template>
        <aside id="player-information-panel" class="player-col-side cczj-flex cczj-flex-col cczj-overflow-hidden" :class="{ collapsed: collapsed }" :inert="!mobile&&collapsed" :aria-hidden="!mobile&&collapsed?true:undefined">
          <div v-if="mobile" class="cczj-mobile-player-tabs" role="tablist" :aria-label="t('mobile.playerContent')">
            <button id="player-overview-tab" role="tab" :aria-selected="mobilePlayerTab === 'overview'" aria-controls="player-overview-panel" @click="mobilePlayerTab = 'overview'">{{ t('mobile.playerOverview') }}</button>
            <button id="player-comments-tab" role="tab" :aria-selected="mobilePlayerTab === 'comments'" aria-controls="player-comments-panel" @click="showMobileComments">{{ t('mobile.playerComments') }}</button>
          </div>
          <section v-if="mobile" v-show="mobilePlayerTab === 'comments'" id="player-comments-panel" class="cczj-mobile-player-comments" role="tabpanel" aria-labelledby="player-comments-tab">
            <DoubanComments v-if="mobileCommentsVisited && mobileCommentsId" :douban-id="mobileCommentsId" />
            <p v-else-if="!mobileCommentsId" class="cczj-mobile-comments-unavailable">{{ t('mobile.commentsUnavailable') }}</p>
          </section>
          <!-- 顶部卡：标题 + 年份/地区/分类 + 简介 + 收藏按钮 -->
          <div v-show="!mobile || mobilePlayerTab === 'overview'" :id="mobile ? 'player-overview-panel' : undefined" :role="mobile ? 'tabpanel' : undefined" :aria-labelledby="mobile ? 'player-overview-tab' : undefined" class="side-header cczj-flex cczj-flex-col cczj-gap-3 cczj-p-4 cczj-mt-0 cczj-mb-0">
            <div class="side-top-bar cczj-flex cczj-items-center cczj-justify-between cczj-gap-3">
              <h1 class="side-title cczj-text-lg cczj-font-semibold cczj-truncate cczj-flex-1">{{ details.video?.vod_name || t('player.videoPlayback') }}</h1>
              <div v-if="!mobile" class="side-top-actions cczj-flex cczj-items-center cczj-gap-2 cczj-flex-shrink-0">
                <button class="close-btn-panel cczj-p-2 cczj-rounded cczj-transition cczj-cursor-pointer" :title="t('common.minimize')" :aria-label="t('common.minimize')" @click="onMinimizeApp">
                  <Icon name="minimize" :size="14" />
                </button>
                <Button variant="text" size="sm" class="close-btn-panel cczj-p-2 cczj-rounded cczj-transition cczj-cursor-pointer" :title="t('common.close')" @click="emit('close')">
                  <Icon name="x" :size="18" />
                </Button>
              </div>
            </div>
            <div class="side-meta cczj-flex cczj-flex-wrap cczj-gap-2">
              <span v-if="mobile && (details.video?.vod_douban_score || details.video?.vod_score)" class="side-meta-chip cczj-mobile-overview-score"><Icon name="star" :size="12" />{{details.video?.vod_douban_score || details.video?.vod_score}}</span>
              <span v-if="details.video?.vod_year" class="side-meta-chip cczj-px-2 cczj-py-1 cczj-text-xs cczj-rounded cczj-bg-secondary">{{ details.video.vod_year }}</span>
              <span v-if="details.video?.vod_area" class="side-meta-chip cczj-px-2 cczj-py-1 cczj-text-xs cczj-rounded cczj-bg-secondary">{{ details.video.vod_area }}</span>
              <span v-if="details.video?.type_name" class="side-meta-chip cczj-px-2 cczj-py-1 cczj-text-xs cczj-rounded cczj-bg-secondary">{{ details.video.type_name }}</span>
            </div>
            <p v-if="details.overview" class="side-blurb cczj-text-sm cczj-text-muted cczj-line-clamp-3 cczj-mt-0 cczj-mb-0">{{ details.overview }}</p>
          </div>

          <!-- 选源区 + 选集区 -->
          <section v-show="!mobile || mobilePlayerTab === 'overview'" class="side-section cczj-flex-1 cczj-overflow-y-auto cczj-p-4 cczj-flex cczj-flex-col">
            <!-- 源列表 -->
            <template v-if="!mobile">
            <div class="side-section-title cczj-flex cczj-items-center cczj-justify-between cczj-gap-2 cczj-mb-3">
              <div class="side-section-title-left cczj-flex cczj-items-center cczj-gap-2">
                <span class="bullet cczj-w-2 cczj-h-2 cczj-rounded-full cczj-bg-accent"></span>
                <span class="cczj-text-sm cczj-font-medium">{{ t('player.playSources') }}</span>
              </div>
              <div class="side-section-right cczj-flex cczj-items-center cczj-gap-2">
                <span class="side-count cczj-text-xs cczj-text-muted">{{ t('player.availableSources', { count: catalog.readyCount }) }}</span>
                <span v-if="catalog.probingCount > 0" class="side-count cczj-text-xs cczj-text-muted">{{ t('player.probePendingCount', { count: catalog.probingCount }) }}</span>
              </div>
            </div>

              <PlaybackRouteList dark :sources="catalog.items" :active-key="catalog.activeKey" :active-line="details.lines[details.activeLineIndex]?.index??-1" :lines="details.lines" :busy="catalog.loading" :selectable="catalog.selectable" @choose="(key,line)=>emit('source',key,line)" />
              <Button class="cczj-mt-4" variant="text" size="sm" :loading="catalog.refreshing" @click="catalog.retry">{{t('playLine.retryQuality')}}</Button>
              <p class="cczj-text-xs cczj-text-muted cczj-mt-2">{{t('playLine.cooldownHint')}}</p>
            </template>

            <!-- 选集区（仅当有剧集时显示） -->
            <template v-if="episodeList.items.length > 0">
              <div class="side-section-title cczj-flex cczj-items-center cczj-justify-between cczj-gap-2 cczj-mt-4 cczj-mb-3">
                <div class="side-section-title-left cczj-flex cczj-items-center cczj-gap-2">
                  <span class="bullet cczj-w-2 cczj-h-2 cczj-rounded-full cczj-bg-accent"></span>
                  <span class="cczj-text-sm cczj-font-medium">{{ t('detail.episodes') }}</span>
                  <span class="side-count cczj-text-xs cczj-text-muted">{{ t('detail.totalEpisodes', { count: episodeList.items.length }) }}</span>
                </div>
                <div class="side-section-right">
                  <Button variant="text" size="sm" class="sort-toggle-btn" :title="episodeList.ascending ? t('detail.sortAscTip') : t('detail.sortDescTip')"
                    @click="emit('sort')">
                    <Icon :name="episodeList.ascending ? 'chevron-down' : 'chevron-up'" :size="12" />
                    <span class="sort-label cczj-text-xs">{{ episodeList.ascending ? t('detail.ascOrder') : t('detail.descOrder') }}</span>
                  </Button>
                </div>
              </div>

              <div class="ep-grid cczj-grid cczj-gap-2" :class="{ 'cczj-episode-grid-detailed': detailedEpisodeLabels }">
                <button v-for="(ep, i) in episodeList.items" :key="episodeList.originalIndex(i)" class="ep-item cczj-relative cczj-px-3 cczj-py-2 cczj-rounded cczj-transition cczj-cursor-pointer cczj-text-sm" :class="{
                  active: episodeList.originalIndex(i) === episodeList.current,
                  watched: episodeList.watched(episodeList.originalIndex(i)),
                  future: episodeList.originalIndex(i) > episodeList.current && !episodeList.watched(episodeList.originalIndex(i)),
                }" :aria-current="episodeList.originalIndex(i) === episodeList.current ? 'step' : undefined" @click="emit('episode',episodeList.originalIndex(i))"
                  :title="episodeList.label(episodeList.originalIndex(i), ep) + (episodeList.percent(episodeList.originalIndex(i)) > 0 ? t('detail.watchedProgress', { pct: Math.round(episodeList.percent(episodeList.originalIndex(i))) }) : '')">
                  <EpisodeLabel class="ep-item-num" :label="episodeList.label(episodeList.originalIndex(i), ep)" />
                  <span v-show="episodeList.originalIndex(i) === episodeList.current" class="ep-playing-badge cczj-absolute cczj-top-1 cczj-right-1 cczj-flex">
                    <span class="bar b1 cczj-bg-accent cczj-rounded"></span>
                    <span class="bar b2 cczj-bg-accent cczj-rounded"></span>
                    <span class="bar b3 cczj-bg-accent cczj-rounded"></span>
                  </span>
                  <span v-show="episodeList.watched(episodeList.originalIndex(i)) && episodeList.percent(episodeList.originalIndex(i)) > 0" class="ep-watched-progress cczj-absolute cczj-bottom-0 cczj-left-0 cczj-bg-accent cczj-rounded"
                    :style="{ width: episodeList.percent(episodeList.originalIndex(i)) + '%' }"></span>
                  <span v-show="episodeList.watched(episodeList.originalIndex(i)) && episodeList.percent(episodeList.originalIndex(i)) > 0" class="ep-watched-pct cczj-absolute cczj-right-1 cczj-text-xs cczj-text-muted">{{
                    Math.round(episodeList.percent(episodeList.originalIndex(i))) }}%</span>
                </button>
              </div>
            </template>
            <div v-else-if="!catalog.loading" class="side-empty cczj-text-center cczj-py-8 cczj-text-muted cczj-text-sm">{{ t('player.noPlayableEpisodes') }}</div>
          </section>
        </aside>
</template>
<style scoped src="../../styles/components/player-information.css"></style>
