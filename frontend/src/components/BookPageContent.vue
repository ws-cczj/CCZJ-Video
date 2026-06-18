<script setup lang="ts">
/**
 * BookPageContent — 书页内容子组件（重写版）
 *
 * 渲染轮播图每一页的内容：
 * - 左侧：海报图片（顶部突出详情区上沿约 16px，带柔和阴影）
 * - 右侧：详情信息（背景从透明渐变到深色，与图片自然融合）
 * - 右下角：克制的三角形折角（点击触发翻页，hover 加深）
 *
 * 设计原则：图片仅轻微突出、折角小巧、详情顶部透明过渡。
 */
import { Button } from './ui'
import Icon from './Icon.vue'
import type { Video } from '../types'

interface Props {
  slide: Video
  imageUrl: string
  /** 是否显示右下角折角（仅当前页显示） */
  showCornerCurl: boolean
  /** 折角是否被悬停 */
  isCornerHovered: boolean
  /** 是否正在翻页（翻页时禁用交互） */
  isFlipping: boolean
  /** 是否正在向后翻页 */
  isFlippingNext?: boolean
  /** 图片顶部突出高度（px） */
  imageProtrude: number
}

const props = defineProps<Props>()

const emit = defineEmits<{
  cornerClick: []
  cornerHover: [value: boolean]
  goDetail: [video: Video]
}>()

const imageWidthPct = 38

function onCornerEnter() {
  if (!props.isFlipping) emit('cornerHover', true)
}
function onCornerLeave() {
  emit('cornerHover', false)
}
function onCornerClick(e: Event) {
  e.stopPropagation()
  if (!props.isFlipping) emit('cornerClick')
}
</script>

<template>
  <div class="bpc-root">
    <!-- ═══ 左侧：海报图片（顶部突出）═══
         图片区顶部向上突出 imageProtrude，突破详情区上沿，
         配柔和投影产生"浮起"感。 -->
    <div class="bpc-image-area" :style="{
      width: imageWidthPct + '%',
      top: -imageProtrude + 'px',
      height: 'calc(100% + ' + imageProtrude + 'px)',
    }">
      <img
        :src="imageUrl"
        :alt="slide?.vod_name || ''"
        class="bpc-image"
        draggable="false"
        referrerpolicy="no-referrer"
        @error="(e: any) => e.target.style.opacity = 0.3"
      />
      <!-- 图片右侧渐变：向详情区过渡（避免硬边） -->
      <div class="bpc-image-right-fade" />
      <!-- 图片底部渐变 -->
      <div class="bpc-image-bottom-fade" />
    </div>

    <!-- ═══ 右侧：详情区 ═══
         关键：背景从左侧透明渐变到右侧深色，顶部也是透明渐变，
         这样图片顶部突出区与详情区上沿之间不会出现黑色硬块。
         详情区不向上延伸（top:0），让图片突出区清晰可见。 -->
    <div class="bpc-detail" :style="{
      left: (imageWidthPct + 1) + '%',
      clipPath: showCornerCurl
        ? 'polygon(0 0, 100% 0, 100% calc(100% - 30px), calc(100% - 30px) 100%, 0 100%)'
        : undefined,
    }">
      <!-- 背景层：左侧透明（露出图片过渡）、顶部透明（消除黑块）、右下深色 -->
      <div class="bpc-detail-bg" />
      <!-- 内容 -->
      <div class="bpc-detail-content" :style="{ paddingTop: imageProtrude + 8 + 'px' }">
        <div class="bpc-badge">热播推荐</div>
        <h2 class="bpc-title">{{ slide?.vod_name || '' }}</h2>
        <p class="bpc-desc">{{ slide?.vod_content?.replace(/<[^>]+>/g, '') || slide?.vod_remarks || '' }}</p>
        <div class="bpc-tags">
          <span v-if="slide?.type_name" class="bpc-tag">{{ slide.type_name }}</span>
          <span v-if="slide?.vod_year" class="bpc-tag">{{ slide.vod_year }}</span>
          <span v-if="slide?.vod_area" class="bpc-tag">{{ slide.vod_area }}</span>
          <span v-if="slide?.vod_score && Number(slide.vod_score) > 0" class="bpc-tag bpc-tag-score">{{ slide.vod_score }}分</span>
          <span v-if="slide?.vod_remarks" class="bpc-tag bpc-tag-remarks">{{ slide.vod_remarks }}</span>
        </div>
        <p v-if="slide?.vod_actor" class="bpc-actor">演员：{{ slide.vod_actor.split(',').slice(0, 4).join(' / ') }}</p>
        <p v-if="slide?.vod_director" class="bpc-director">导演：{{ slide.vod_director.split(',').slice(0, 2).join(' / ') }}</p>
      </div>
      <div class="bpc-actions">
        <Button variant="primary" size="sm" @click.stop="emit('goDetail', slide)">
          <Icon name="play" :size="16" />
          <span>立即播放</span>
        </Button>
        <Button variant="secondary" size="sm" @click.stop="emit('goDetail', slide)">
          <Icon name="info" :size="14" />
          <span>详情</span>
        </Button>
      </div>
    </div>

    <!-- ═══ 右下角折角（克制的小三角，非大卷起）═══
         30px 三角形，hover 加深 + 微微抬起。点击触发翻页。 -->
    <template v-if="showCornerCurl">
      <!-- 折角热区（比折角大，便于触发） -->
      <div
        class="bpc-corner-hit"
        @mouseenter="onCornerEnter"
        @mouseleave="onCornerLeave"
        @click="onCornerClick"
      />
      <!-- 折角本体（三角形） -->
      <div
        class="bpc-corner"
        :class="{ 'is-hovered': isCornerHovered, 'flip': isFlipping && isFlippingNext }"
        @mouseenter="onCornerEnter"
        @mouseleave="onCornerLeave"
        @click="onCornerClick"
      >
        <div class="bpc-corner-face" />
        <div class="bpc-corner-shine" />
      </div>
    </template>
  </div>
</template>

<style scoped>
.bpc-root {
  position: relative;
  height: 100%;
  width: 100%;
  overflow: hidden;
  border-radius: 6px;
}

/* ═══ 图片区 ═══ */
.bpc-image-area {
  position: absolute;
  left: 0;
  bottom: 0;
  z-index: 5;
  /* 用 box-shadow 替代 filter drop-shadow（GPU 加速更好，不触发 repaint） */
}
.bpc-image-area::after {
  content: '';
  position: absolute;
  inset: 0;
  border-radius: 6px 0 0 6px;
  box-shadow: 0 6px 14px rgba(0, 0, 0, 0.45);
  pointer-events: none;
  z-index: -1;
}
.bpc-image {
  width: 100%;
  height: 100%;
  object-fit: cover;
  border-radius: 6px 0 0 6px;
  pointer-events: none;
  display: block;
}
/* 图片右侧渐变：让图片与详情区交界处柔和过渡 */
.bpc-image-right-fade {
  position: absolute;
  top: 0;
  right: 0;
  bottom: 0;
  width: 40%;
  pointer-events: none;
  background: linear-gradient(to right,
    transparent 0%,
    rgba(15, 15, 28, 0.15) 30%,
    rgba(15, 15, 28, 0.4) 60%,
    rgba(15, 15, 28, 0.75) 85%,
    rgba(15, 15, 28, 0.9) 100%);
}
.bpc-image-bottom-fade {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: 90px;
  pointer-events: none;
  background: linear-gradient(to top,
    rgba(15, 15, 28, 0.6) 0%,
    transparent 100%);
}

/* ═══ 详情区 ═══ */
.bpc-detail {
  position: absolute;
  right: 0;
  bottom: 0;
  top: -24px;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  padding: 20px 24px;
  min-width: 0;
  z-index: 4;
  overflow: hidden;
  border-radius: 0 6px 6px 0;
}
/* 背景层：左侧透明（与图片融合）→ 右侧深色；顶部完全透明（消除黑块）→ 底部深色 */
.bpc-detail-bg {
  position: absolute;
  inset: 0;
  z-index: -1;
  background:
    /* 横向：左透明右深，让图片区透过来 */
    linear-gradient(to right,
      rgba(15, 15, 28, 0.0) 0%,
      rgba(15, 15, 28, 0.25) 10%,
      rgba(15, 15, 28, 0.75) 25%,
      rgba(12, 12, 24, 0.96) 100%),
    /* 纵向：顶部完全透明（突出区可见）→ 底部深色 */
    linear-gradient(to bottom,
      rgba(15, 15, 28, 0.0) 0%,
      rgba(15, 15, 28, 0.0) 20%,
      rgba(15, 15, 28, 0.3) 35%,
      rgba(12, 12, 24, 0.9) 100%),
    #0c0c18;
}
.bpc-detail-content {
  position: relative;
  z-index: 2;
}
.bpc-badge {
  display: inline-block;
  padding: 2px 8px;
  background: rgba(239, 68, 68, 0.85);
  color: #fff;
  border-radius: 3px;
  font-size: 10px;
  font-weight: 600;
  margin-bottom: 10px;
  letter-spacing: 0.5px;
}
.bpc-title {
  font-size: 19px;
  font-weight: 700;
  color: #fff;
  line-height: 1.3;
  margin: 0 0 10px 0;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}
.bpc-desc {
  font-size: 12.5px;
  color: rgba(255, 255, 255, 0.5);
  line-height: 1.6;
  margin: 0 0 14px 0;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
}
.bpc-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 10px;
}
.bpc-tag {
  padding: 2px 8px;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.08);
  color: rgba(255, 255, 255, 0.6);
  font-size: 11px;
}
.bpc-tag-score {
  background: rgba(245, 158, 11, 0.2);
  color: #fcd34d;
  font-weight: 600;
}
.bpc-tag-remarks {
  background: rgba(16, 185, 129, 0.15);
  color: rgba(52, 211, 153, 0.85);
}
.bpc-actor,
.bpc-director {
  font-size: 11.5px;
  color: rgba(255, 255, 255, 0.35);
  margin: 0 0 3px 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.bpc-actions {
  display: flex;
  gap: 10px;
  margin-top: 12px;
  position: relative;
  z-index: 2;
}

/* ═══ 右下角3D卷页效果 ═══ */
.bpc-corner-hit {
  position: absolute;
  bottom: 0;
  right: 0;
  width: 60px;
  height: 60px;
  z-index: 31;
  cursor: pointer;
}
.bpc-corner {
  position: absolute;
  bottom: 0;
  right: 0;
  width: 40px;
  height: 40px;
  z-index: 30;
  cursor: pointer;
  transform-style: preserve-3d;
  transition: transform 0.4s cubic-bezier(0.4, 0, 0.2, 1);
  transform-origin: right bottom;
}
.bpc-corner.is-hovered {
  transform: rotate(-15deg) rotateX(15deg) translateY(-3px);
}
.bpc-corner-face {
  position: absolute;
  inset: 0;
  clip-path: polygon(100% 0, 100% 100%, 0 100%);
  background: linear-gradient(225deg,
    rgba(200, 200, 220, 0.95) 0%,
    rgba(160, 160, 185, 0.92) 30%,
    rgba(110, 110, 135, 0.88) 60%,
    rgba(70, 70, 95, 0.85) 100%);
  box-shadow: 
    -2px -2px 6px rgba(255, 255, 255, 0.15),
    2px 2px 6px rgba(0, 0, 0, 0.3);
}
.bpc-corner-shine {
  position: absolute;
  top: 0;
  right: 0;
  width: 100%;
  height: 100%;
  clip-path: polygon(100% 0, 100% 100%, 0 100%);
  background: linear-gradient(to left top,
    rgba(255, 255, 255, 0.7) 0%,
    rgba(255, 255, 255, 0.2) 30%,
    transparent 60%);
  transform: translateZ(2px);
}
/* 卷页阴影：模拟翻开的阴影 */
.bpc-corner::before {
  content: '';
  position: absolute;
  bottom: 0;
  right: 0;
  width: 40px;
  height: 40px;
  clip-path: polygon(100% 0, 100% 100%, 0 100%);
  background: rgba(0, 0, 0, 0.4);
  transform: translateZ(-3px);
  opacity: 0.3;
  transition: opacity 0.3s ease;
}
.bpc-corner.is-hovered::before {
  opacity: 0.6;
}
/* 翻页时的动画 */
@keyframes cornerFlip {
  0% {
    transform: rotate(0deg) rotateX(0deg) translateY(0);
    opacity: 1;
  }
  50% {
    transform: rotate(-30deg) rotateX(45deg) translateY(-8px);
    opacity: 0.8;
  }
  100% {
    transform: rotate(-45deg) rotateX(90deg) translateY(-15px);
    opacity: 0;
  }
}
.bpc-corner.flip {
  animation: cornerFlip 0.5s cubic-bezier(0.4, 0, 0.2, 1) forwards;
}
</style>
