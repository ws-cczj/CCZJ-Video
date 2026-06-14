import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import UnoCSS from 'unocss/vite'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [
    vue(),
    UnoCSS(), // UnoCSS 按需生成原子样式（替代 Tailwind CSS）
  ],
  server: {
    host: '127.0.0.1', // 强制 IPv4，避免 wails3 用 127.0.0.1 连接时因 IPv6 绑定失败
    port: 9245, // 与 wails3 dev 保持一致（WAILS_VITE_PORT）
    strictPort: true,
  },
  define: {
    // Vue 3.3+ 的 esm-bundler 构建需要在编译期注入这些 flag，
    // 以获得更好的 tree-shaking，并消除控制台警告。
    __VUE_OPTIONS_API__: JSON.stringify(true),
    __VUE_PROD_DEVTOOLS__: JSON.stringify(false),
    __VUE_PROD_HYDRATION_MISMATCH_DETAILS__: JSON.stringify(false),
  },
  build: {
    // 优化代码分割，避免单个 chunk 过大
    rollupOptions: {
      output: {
        manualChunks: {
          // hls.js 单独分割（约 557KB）
          'hls': ['hls.js'],
          // Vue 核心库
          'vue-vendor': ['vue', 'vue-router'],
        },
      },
    },
    // 提高 chunk 大小警告阈值
    chunkSizeWarningLimit: 600,
  },
})
