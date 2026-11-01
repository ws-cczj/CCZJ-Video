<script setup lang="ts">
import type { ChapterTextDTO } from '../../api/library'
import type { NovelChapter } from '../domain'
import Icon from '../../components/Icon.vue'

defineProps<{ chapter:NovelChapter; text:ChapterTextDTO }>()
</script>

<template>
  <h1>{{ chapter.title }}</h1>
  <div v-if="text.warnings?.length" class="novel-reading-warnings" role="note"><p v-for="(warning,i) in text.warnings" :key="i"><Icon name="alert-triangle" :size="14" />{{ warning }}</p></div>
  <p v-for="(paragraph,i) in text.paragraphs" :key="i" class="novel-reading-paragraph">{{ paragraph }}</p>
</template>
