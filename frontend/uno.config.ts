import {
  defineConfig,
  presetWind,
} from 'unocss'

export default defineConfig({
  presets: [
    presetWind({
      // Tailwind 兼容模式：flex, items-center, gap-4, p-4, rounded-lg, etc.
    }),
  ],
  theme: {
    // 将项目自定义的 CSS 变量映射为 UnoCSS 设计 token
    colors: {
      accent: 'var(--accent)',
      'accent-dim': 'var(--accent-dim)',
      'accent-contrast': 'var(--accent-contrast)',
      'accent-alpha-10': 'var(--accent-alpha-10)',
      'accent-alpha-20': 'var(--accent-alpha-20)',
      'accent-alpha-35': 'var(--accent-alpha-35)',

      danger: 'var(--danger)',
      warn: 'var(--warn)',
      success: 'var(--success)',

      bg: {
        app: 'var(--bg-app)',
        DEFAULT: 'var(--bg-secondary)',
        secondary: 'var(--bg-secondary)',
        card: 'var(--bg-card)',
        hover: 'var(--bg-hover)',
      },

      text: {
        primary: 'var(--text-primary)',
        secondary: 'var(--text-secondary)',
        muted: 'var(--text-muted)',
      },

      border: {
        DEFAULT: 'var(--border)',
        strong: 'var(--border-strong)',
      },
    },
    fontFamily: {
      sans: ['"Nunito"', '"PingFang SC"', '"Microsoft YaHei"', 'system-ui', 'sans-serif'],
      mono: ['Menlo', 'Monaco', 'Consolas', 'monospace'],
    },
    borderRadius: {
      lg: '10px',
      xl: '14px',
    },
    boxShadow: {
      card: '0 2px 8px rgba(0, 0, 0, 0.06)',
      'accent-soft': '0 2px 8px var(--accent-alpha-20)',
      'accent-hover': '0 4px 14px var(--accent-alpha-35)',
    },
  },
  content: {
    pipeline: {
      include: [
        /\.(vue|svelte|[jt]sx|mdx?|astro|elm|php|phtml|html)($|\?)/,
        /\.(ts|js)($|\?)/,
      ],
    },
  },
  safelist: [],
})
