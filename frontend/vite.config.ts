import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// https://vitejs.dev/config/
export default defineConfig(({ command }) => ({
  plugins: [
    vue(),
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
  // 生产构建剔除 console.log / console.debug，保留 warn/error 用于排错。
  // 必须放在根级 esbuild 而不是 build.esbuild：后者不是 Vite 的选项，写在里面
  // 等于没写，调试输出会原样打进发布的包里。
  // 也不用 drop: ['console'] —— 那会连 warn/error 一起丢掉；pure 只删结果未被使用的
  // 这两个调用，且只在 build 时生效，开发模式仍然要看得见日志。
  esbuild: command === 'build' ? { pure: ['console.log', 'console.debug'] } : {},
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
}))
