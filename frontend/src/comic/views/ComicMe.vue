<script setup lang="ts">
import {useI18n} from 'vue-i18n'
import {useComicState} from '../state'
import Icon from '../../components/Icon.vue'
const {t}=useI18n(),state=useComicState()
</script>
<template><div class="comic-page comic-me"><div class="comic-profile"><span><Icon name="panels" :size="34"/></span><div><h1>{{ t('comic.readerName') }}</h1><p>{{ t('comic.profileHint') }}</p></div></div><div class="comic-stats"><router-link to="/comic/library"><strong>{{ state.followed.length }}</strong><span>{{ t('comic.myFollowing') }}</span></router-link><router-link to="/comic/library"><strong>{{ state.recent.length }}</strong><span>{{ t('comic.readSeries') }}</span></router-link></div><router-link to="/comic/settings" class="comic-menu-row"><Icon name="settings" :size="22"/><span><strong>{{ t('sidebar.settings') }}</strong><small>{{ t('comic.settingsHint') }}</small></span><Icon name="chevron-right" :size="20"/></router-link><router-link v-if="state.recent[0]" :to="state.resumePath(state.recent[0])" class="comic-menu-row"><Icon name="clock" :size="22"/><span><strong>{{ t('comic.continueReading') }}</strong><small>{{ state.recent[0].title }}</small></span><Icon name="chevron-right" :size="20"/></router-link><p class="comic-demo-note">{{ t('comic.localHint') }}</p></div></template>
