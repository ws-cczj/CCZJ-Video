<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useNovelReading } from '../readingState'
import Icon from '../../components/Icon.vue'
const reading=useNovelReading(),{t}=useI18n()
const lastPath=computed(()=>reading.lastRead?reading.readingPath(reading.lastRead.key):null)
</script>
<template><div class="novel-page novel-me"><div class="novel-reader-profile"><span class="novel-profile-icon"><Icon name="book-open" :size="30" /></span><div><h1>{{ t('novel.readerName') }}</h1><p>{{ t('novel.readerGreeting') }}</p></div></div><div class="novel-reading-stats"><router-link to="/novel/shelf"><strong>{{ reading.shelf.length }}</strong><span>{{ t('novel.shelfBooks') }}</span></router-link><div><strong>{{ Object.keys(reading.progress).length }}</strong><span>{{ t('novel.booksStarted') }}</span></div></div><router-link v-if="reading.lastRead && lastPath" :to="lastPath" class="novel-continue"><Icon name="clock" :size="20" /><div><small>{{ t('novel.recentReading') }}</small><strong>{{ reading.lastRead.progress.title }}</strong></div><Icon name="chevron-right" :size="18" /></router-link><nav class="novel-menu"><router-link to="/novel/settings"><Icon name="settings" :size="20" /><span>{{ t('sidebar.settings') }}<small>{{ t('novel.settingsHint') }}</small></span><Icon name="chevron-right" :size="18" /></router-link><router-link to="/novel/shelf"><Icon name="bookmark" :size="20" /><span>{{ t('novel.myShelf') }}<small>{{ t('novel.shelfLocal') }}</small></span><Icon name="chevron-right" :size="18" /></router-link></nav></div></template>
