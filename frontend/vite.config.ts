import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { cczjUtilities } from './build/cczjUtilities'

// https://vitejs.dev/config/
export default defineConfig(({ command, mode }) => ({
  plugins: [
    vue(),
  ],
  css: { postcss: { plugins: [cczjUtilities()] } },
  server: {
    host: '127.0.0.1', // 强制 IPv4，避免 wails3 用 127.0.0.1 连接时因 IPv6 绑定失败
    port: mode === 'android' ? Number(process.env.CCZJ_ANDROID_DEV_PORT || 19545) : 9245,
    hmr: mode === 'android' ? { protocol: 'ws', host: '127.0.0.1', clientPort: Number(process.env.CCZJ_ANDROID_DEV_PORT || 19545) } : undefined,
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
    // Preserve the pre-upgrade browser syntax baseline; toolchain upgrades do
    // not raise the Android WebView or desktop runtime requirement.
    target: ['es2020', 'chrome87', 'edge88', 'firefox78', 'safari14'],
    cssTarget: ['chrome87', 'edge88', 'firefox78', 'safari14'],
    rolldownOptions: {
      treeshake: command === 'build' ? { manualPureFunctions: ['console.log', 'console.debug'] } : true,
      output: {
        codeSplitting: {
          groups: [
            { name: 'hls', test: /[\\/]node_modules[\\/]hls\.js[\\/]/ },
            { name: 'vue-vendor', test: /[\\/]node_modules[\\/](?:@vue|vue|vue-router)[\\/]/ },
          ],
        },
      },
    },
    // 提高 chunk 大小警告阈值
    chunkSizeWarningLimit: 600,
  },
}))
