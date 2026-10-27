# CCZJ 原子样式速查手册

> 由 `scripts/gen-cczj-vscode-data.mjs` 从 `frontend/src/styles/cczj-utilities.css` 生成，请修改源 CSS 后重新生成。

模板使用 `class="cczj-flex cczj-gap-4"`；作用域、状态或响应式规则可写 `.toolbar { @apply cczj-flex cczj-gap-4; }`。两者读取同一份定义。用法及边界见 [CCZJ 样式复用](cczj-styles.md)。

间距数字沿用 2px 单位；历史尺寸类存在固定像素与 4px 单位两套命名，表中实际声明为准，不能按类名猜测。旧类保持兼容，不改变已有页面尺寸。

### 1. 显示 (Display)

| 类名 | 生效样式 |
| --- | --- |
| `cczj-flex` | `display: flex;` |
| `cczj-inline-flex` | `display: inline-flex;` |
| `cczj-grid` | `display: grid;` |
| `cczj-grid-cols-2` | `grid-template-columns: repeat(2, minmax(0, 1fr));` |
| `cczj-block` | `display: block;` |
| `cczj-inline-block` | `display: inline-block;` |
| `cczj-hidden` | `display: none;` |

### 2. Flexbox 布局

| 类名 | 生效样式 |
| --- | --- |
| `cczj-flex-row` | `flex-direction: row;` |
| `cczj-flex-col` | `flex-direction: column;` |
| `cczj-flex-row-rev` | `flex-direction: row-reverse;` |
| `cczj-flex-col-rev` | `flex-direction: column-reverse;` |
| `cczj-flex-wrap` | `flex-wrap: wrap;` |
| `cczj-flex-nowrap` | `flex-wrap: nowrap;` |
| `cczj-flex-1` | `flex: 1;` |
| `cczj-flex-auto` | `flex: auto;` |
| `cczj-flex-none` | `flex: none;` |
| `cczj-flex-grow` | `flex-grow: 1;` |
| `cczj-flex-shrink-0` | `flex-shrink: 0;` |
| `cczj-justify-start` | `justify-content: flex-start;` |
| `cczj-justify-center` | `justify-content: center;` |
| `cczj-justify-end` | `justify-content: flex-end;` |
| `cczj-justify-between` | `justify-content: space-between;` |
| `cczj-justify-around` | `justify-content: space-around;` |
| `cczj-justify-evenly` | `justify-content: space-evenly;` |
| `cczj-items-start` | `align-items: flex-start;` |
| `cczj-items-center` | `align-items: center;` |
| `cczj-items-end` | `align-items: flex-end;` |
| `cczj-items-stretch` | `align-items: stretch;` |
| `cczj-items-baseline` | `align-items: baseline;` |
| `cczj-self-start` | `align-self: flex-start;` |
| `cczj-self-center` | `align-self: center;` |
| `cczj-self-end` | `align-self: flex-end;` |
| `cczj-self-stretch` | `align-self: stretch;` |

### 3. 间距 (Gap)

| 类名 | 生效样式 |
| --- | --- |
| `cczj-gap-1` | `gap: 2px;` |
| `cczj-gap-2` | `gap: 4px;` |
| `cczj-gap-3` | `gap: 6px;` |
| `cczj-gap-4` | `gap: 8px;` |
| `cczj-gap-5` | `gap: 10px;` |
| `cczj-gap-6` | `gap: 12px;` |
| `cczj-gap-7` | `gap: 14px;` |
| `cczj-gap-8` | `gap: 16px;` |
| `cczj-gap-10` | `gap: 20px;` |
| `cczj-gap-12` | `gap: 24px;` |

### 4. Margin

| 类名 | 生效样式 |
| --- | --- |
| `cczj-mt-0` | `margin-top: 0;` |
| `cczj-mt-1` | `margin-top: 2px;` |
| `cczj-mt-2` | `margin-top: 4px;` |
| `cczj-mt-3` | `margin-top: 6px;` |
| `cczj-mt-4` | `margin-top: 8px;` |
| `cczj-mt-5` | `margin-top: 10px;` |
| `cczj-mt-6` | `margin-top: 12px;` |
| `cczj-mt-8` | `margin-top: 16px;` |
| `cczj-mt-10` | `margin-top: 20px;` |
| `cczj-mt-12` | `margin-top: 24px;` |
| `cczj-mt-auto` | `margin-top: auto;` |
| `cczj-mb-0` | `margin-bottom: 0;` |
| `cczj-mb-1` | `margin-bottom: 2px;` |
| `cczj-mb-2` | `margin-bottom: 4px;` |
| `cczj-mb-3` | `margin-bottom: 6px;` |
| `cczj-mb-4` | `margin-bottom: 8px;` |
| `cczj-mb-5` | `margin-bottom: 10px;` |
| `cczj-mb-6` | `margin-bottom: 12px;` |
| `cczj-mb-7` | `margin-bottom: 14px;` |
| `cczj-mb-8` | `margin-bottom: 16px;` |
| `cczj-mb-10` | `margin-bottom: 20px;` |
| `cczj-mb-12` | `margin-bottom: 24px;` |
| `cczj-mb-auto` | `margin-bottom: auto;` |
| `cczj-ml-0` | `margin-left: 0;` |
| `cczj-ml-1` | `margin-left: 2px;` |
| `cczj-ml-2` | `margin-left: 4px;` |
| `cczj-ml-3` | `margin-left: 6px;` |
| `cczj-ml-4` | `margin-left: 8px;` |
| `cczj-ml-5` | `margin-left: 10px;` |
| `cczj-ml-6` | `margin-left: 12px;` |
| `cczj-ml-8` | `margin-left: 16px;` |
| `cczj-ml-auto` | `margin-left: auto;` |
| `cczj-mr-0` | `margin-right: 0;` |
| `cczj-mr-1` | `margin-right: 2px;` |
| `cczj-mr-2` | `margin-right: 4px;` |
| `cczj-mr-3` | `margin-right: 6px;` |
| `cczj-mr-4` | `margin-right: 8px;` |
| `cczj-mr-5` | `margin-right: 10px;` |
| `cczj-mr-6` | `margin-right: 12px;` |
| `cczj-mr-8` | `margin-right: 16px;` |
| `cczj-mr-auto` | `margin-right: auto;` |
| `cczj-mx-auto` | `margin-left: auto; margin-right: auto;` |
| `cczj-my-4` | `margin-top: 8px; margin-bottom: 8px;` |
| `cczj-my-6` | `margin-top: 12px; margin-bottom: 12px;` |

### 5. Padding

| 类名 | 生效样式 |
| --- | --- |
| `cczj-pt-2` | `padding-top: 4px;` |
| `cczj-pt-4` | `padding-top: 8px;` |
| `cczj-pt-6` | `padding-top: 12px;` |
| `cczj-pt-8` | `padding-top: 16px;` |
| `cczj-pt-10` | `padding-top: 20px;` |
| `cczj-pb-2` | `padding-bottom: 4px;` |
| `cczj-pb-4` | `padding-bottom: 8px;` |
| `cczj-pb-6` | `padding-bottom: 12px;` |
| `cczj-pb-8` | `padding-bottom: 16px;` |
| `cczj-pb-10` | `padding-bottom: 20px;` |
| `cczj-pl-2` | `padding-left: 4px;` |
| `cczj-pl-4` | `padding-left: 8px;` |
| `cczj-pl-6` | `padding-left: 12px;` |
| `cczj-pl-8` | `padding-left: 16px;` |
| `cczj-pr-2` | `padding-right: 4px;` |
| `cczj-pr-4` | `padding-right: 8px;` |
| `cczj-pr-6` | `padding-right: 12px;` |
| `cczj-pr-8` | `padding-right: 16px;` |
| `cczj-p-2` | `padding: 4px;` |
| `cczj-p-3` | `padding: 6px;` |
| `cczj-p-4` | `padding: 8px;` |
| `cczj-p-5` | `padding: 10px;` |
| `cczj-p-6` | `padding: 12px;` |
| `cczj-p-7` | `padding: 14px;` |
| `cczj-p-8` | `padding: 16px;` |
| `cczj-px-4` | `padding-left: 8px; padding-right: 8px;` |
| `cczj-px-6` | `padding-left: 12px; padding-right: 12px;` |
| `cczj-px-8` | `padding-left: 16px; padding-right: 16px;` |
| `cczj-px-12` | `padding-left: 24px; padding-right: 24px;` |
| `cczj-py-2` | `padding-top: 4px; padding-bottom: 4px;` |
| `cczj-py-4` | `padding-top: 8px; padding-bottom: 8px;` |
| `cczj-py-6` | `padding-top: 12px; padding-bottom: 12px;` |
| `cczj-px-8-py-4` | `padding: 8px 16px;` |
| `cczj-px-6-py-3` | `padding: 6px 12px;` |
| `cczj-px-6-py-4` | `padding: 8px 12px;` |
| `cczj-px-7-py-5` | `padding: 10px 14px;` |
| `cczj-px-5-py-2` | `padding: 3px 10px;` |
| `cczj-px-8-py-6` | `padding: 12px 16px;` |

### 6. 定位 (Position)

| 类名 | 生效样式 |
| --- | --- |
| `cczj-relative` | `position: relative;` |
| `cczj-absolute` | `position: absolute;` |
| `cczj-fixed` | `position: fixed;` |
| `cczj-sticky` | `position: sticky;` |
| `cczj-inset-0` | `top: 0; right: 0; bottom: 0; left: 0;` |
| `cczj-top-0` | `top: 0;` |
| `cczj-right-0` | `right: 0;` |
| `cczj-bottom-0` | `bottom: 0;` |
| `cczj-left-0` | `left: 0;` |

### 7. 尺寸 (Width / Height)

| 类名 | 生效样式 |
| --- | --- |
| `cczj-w-full` | `width: 100%;` |
| `cczj-h-full` | `height: 100%;` |
| `cczj-w-screen` | `width: 100vw;` |
| `cczj-h-screen` | `height: 100vh;` |
| `cczj-min-w-0` | `min-width: 0;` |
| `cczj-min-h-0` | `min-height: 0;` |
| `cczj-max-w-full` | `max-width: 100%;` |
| `cczj-max-h-full` | `max-height: 100%;` |
| `cczj-w-16` | `width: 16px;` |
| `cczj-w-24` | `width: 24px;` |
| `cczj-w-32` | `width: 32px;` |
| `cczj-h-16` | `height: 16px;` |
| `cczj-h-24` | `height: 24px;` |
| `cczj-h-32` | `height: 32px;` |

### 8. 文字排版 (Typography)

| 类名 | 生效样式 |
| --- | --- |
| `cczj-text-xs` | `font-size: calc(10rem / 14);` |
| `cczj-text-sm` | `font-size: calc(12rem / 14);` |
| `cczj-text-base` | `font-size: calc(14rem / 14);` |
| `cczj-text-md` | `font-size: calc(14rem / 14);` |
| `cczj-text-lg` | `font-size: calc(16rem / 14);` |
| `cczj-text-xl` | `font-size: calc(18rem / 14);` |
| `cczj-text-2xl` | `font-size: calc(20rem / 14);` |
| `cczj-text-11` | `font-size: calc(11rem / 14);` |
| `cczj-text-12` | `font-size: calc(12rem / 14);` |
| `cczj-text-13` | `font-size: calc(13rem / 14);` |
| `cczj-font-normal` | `font-weight: 400;` |
| `cczj-font-medium` | `font-weight: 500;` |
| `cczj-font-semibold` | `font-weight: 600;` |
| `cczj-font-bold` | `font-weight: 700;` |
| `cczj-text-left` | `text-align: left;` |
| `cczj-text-center` | `text-align: center;` |
| `cczj-text-right` | `text-align: right;` |
| `cczj-underline` | `text-decoration: underline;` |
| `cczj-line-through` | `text-decoration: line-through;` |
| `cczj-no-underline` | `text-decoration: none;` |
| `cczj-leading-none` | `line-height: 1;` |
| `cczj-leading-tight` | `line-height: 1.25;` |
| `cczj-leading-normal` | `line-height: 1.5;` |
| `cczj-truncate` | `overflow: hidden; text-overflow: ellipsis; white-space: nowrap;` |
| `cczj-line-clamp-2` | `display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;` |
| `cczj-line-clamp-3` | `display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden;` |
| `cczj-whitespace-nowrap` | `white-space: nowrap;` |
| `cczj-whitespace-pre` | `white-space: pre-wrap;` |
| `cczj-break-all` | `word-break: break-all;` |
| `cczj-wrap-anywhere` | `overflow-wrap: anywhere;` |

### 9. 光标 (Cursor)

| 类名 | 生效样式 |
| --- | --- |
| `cczj-cursor-pointer` | `cursor: pointer;` |
| `cczj-cursor-default` | `cursor: default;` |
| `cczj-cursor-grab` | `cursor: grab;` |
| `cczj-cursor-not-allowed` | `cursor: not-allowed;` |

### 10. 溢出 (Overflow)

| 类名 | 生效样式 |
| --- | --- |
| `cczj-overflow-hidden` | `overflow: hidden;` |
| `cczj-overflow-auto` | `overflow: auto;` |
| `cczj-overflow-scroll` | `overflow: scroll;` |
| `cczj-overflow-visible` | `overflow: visible;` |
| `cczj-overflow-x-hidden` | `overflow-x: hidden;` |
| `cczj-overflow-x-auto` | `overflow-x: auto;` |
| `cczj-overflow-y-auto` | `overflow-y: auto;` |
| `cczj-overflow-y-hidden` | `overflow-y: hidden;` |

### 11. 圆角 (Border Radius)

| 类名 | 生效样式 |
| --- | --- |
| `cczj-rounded-xs` | `border-radius: 2px;` |
| `cczj-rounded-sm` | `border-radius: 4px;` |
| `cczj-rounded` | `border-radius: 6px;` |
| `cczj-rounded-md` | `border-radius: 8px;` |
| `cczj-rounded-lg` | `border-radius: 12px;` |
| `cczj-rounded-xl` | `border-radius: 16px;` |
| `cczj-rounded-full` | `border-radius: 9999px;` |
| `cczj-rounded-50` | `border-radius: 50%;` |

### 12. 透明度 (Opacity)

| 类名 | 生效样式 |
| --- | --- |
| `cczj-opacity-0` | `opacity: 0;` |
| `cczj-opacity-40` | `opacity: 0.4;` |
| `cczj-opacity-50` | `opacity: 0.5;` |
| `cczj-opacity-60` | `opacity: 0.6;` |
| `cczj-opacity-70` | `opacity: 0.7;` |
| `cczj-opacity-80` | `opacity: 0.8;` |
| `cczj-opacity-100` | `opacity: 1;` |

### 13. 过渡 (Transition)

| 类名 | 生效样式 |
| --- | --- |
| `cczj-transition-fast` | `transition: all 0.15s ease;` |
| `cczj-transition` | `transition: all 0.2s ease;` |
| `cczj-transition-slow` | `transition: all 0.3s ease;` |
| `cczj-transition-none` | `transition: none;` |

### 14. 用户选择 (User Select)

| 类名 | 生效样式 |
| --- | --- |
| `cczj-select-none` | `user-select: none; -webkit-user-select: none;` |
| `cczj-select-text` | `user-select: text; -webkit-user-select: text;` |

### 15. 指针事件 (Pointer Events)

| 类名 | 生效样式 |
| --- | --- |
| `cczj-pointer-events-none` | `pointer-events: none;` |
| `cczj-pointer-events-auto` | `pointer-events: auto;` |

### 16. 层级 (Z-Index)

| 类名 | 生效样式 |
| --- | --- |
| `cczj-z-0` | `z-index: 0;` |
| `cczj-z-10` | `z-index: 10;` |
| `cczj-z-50` | `z-index: 50;` |
| `cczj-z-100` | `z-index: 100;` |
| `cczj-z-200` | `z-index: 200;` |
| `cczj-z-999` | `z-index: 999;` |
| `cczj-z-max` | `z-index: 999999;` |

### 17. 背景与边框语义快捷 (使用设计令牌)

| 类名 | 生效样式 |
| --- | --- |
| `cczj-bg-primary` | `background-color: var(--bg-primary);` |
| `cczj-bg-secondary` | `background-color: var(--bg-secondary);` |
| `cczj-bg-card` | `background-color: var(--bg-card);` |
| `cczj-bg-hover` | `background-color: var(--bg-hover);` |
| `cczj-bg-accent` | `background-color: var(--accent);` |
| `cczj-bg-danger` | `background-color: var(--danger);` |
| `cczj-bg-transparent` | `background-color: transparent;` |
| `cczj-text-primary` | `color: var(--text-primary);` |
| `cczj-text-secondary` | `color: var(--text-secondary);` |
| `cczj-text-muted` | `color: var(--text-muted);` |
| `cczj-text-accent` | `color: var(--accent);` |
| `cczj-text-danger` | `color: var(--danger);` |
| `cczj-text-success` | `color: var(--success);` |
| `cczj-text-warning` | `color: var(--warning);` |
| `cczj-text-white` | `color: #fff;` |
| `cczj-border` | `border: 1px solid var(--border);` |
| `cczj-border-strong` | `border: 1px solid var(--border-strong);` |
| `cczj-border-bottom` | `border-bottom: 1px solid var(--border);` |
| `cczj-border-top` | `border-top: 1px solid var(--border);` |

### 18. 阴影 (Shadow)

| 类名 | 生效样式 |
| --- | --- |
| `cczj-shadow-sm` | `box-shadow: var(--shadow-sm);` |
| `cczj-shadow` | `box-shadow: var(--shadow);` |
| `cczj-shadow-lg` | `box-shadow: var(--shadow-lg);` |
| `cczj-shadow-none` | `box-shadow: none;` |

### 19. 其他常用

| 类名 | 生效样式 |
| --- | --- |
| `cczj-outline-none` | `outline: none;` |
| `cczj-sr-only` | `position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0, 0, 0, 0); border: 0;` |

### 20. 模板补全类（书写意图为 Tailwind 语义，尺寸/偏移按 value*4px）

| 类名 | 生效样式 |
| --- | --- |
| `cczj-px-2` | `padding-left: 4px; padding-right: 4px;` |
| `cczj-px-3` | `padding-left: 6px; padding-right: 6px;` |
| `cczj-py-1` | `padding-top: 2px; padding-bottom: 2px;` |
| `cczj-py-8` | `padding-top: 16px; padding-bottom: 16px;` |
| `cczj-py-12` | `padding-top: 24px; padding-bottom: 24px;` |
| `cczj-top-1` | `top: 4px;` |
| `cczj-top-2` | `top: 8px;` |
| `cczj-right-1` | `right: 4px;` |
| `cczj-right-2` | `right: 8px;` |
| `cczj-bottom-1` | `bottom: 4px;` |
| `cczj-left-2` | `left: 8px;` |
| `cczj-w-2` | `width: 8px;` |
| `cczj-w-8` | `width: 32px;` |
| `cczj-w-64` | `width: 256px;` |
| `cczj-h-1.5` | `height: 6px;` |
| `cczj-h-2` | `height: 8px;` |
| `cczj-min-w-48` | `min-width: 192px;` |
| `cczj-max-w-72` | `max-width: 288px;` |
| `cczj-max-h-64` | `max-height: 256px;` |
| `cczj-z-20` | `z-index: 20;` |
| `cczj-text-4xl` | `font-size: calc(36rem / 14); line-height: 1.15;` |
| `cczj-rounded-l` | `border-radius: 6px 0 0 6px;` |
| `cczj-bg-border` | `background: var(--border);` |
| `cczj-bg-danger-alpha` | `background: var(--danger-alpha-10);` |
| `cczj-border-dashed` | `border-style: dashed;` |
| `cczj-border-primary` | `border-color: var(--accent);` |
| `cczj-border-gray-300` | `border-color: var(--border);` |
| `cczj-object-cover` | `object-fit: cover;` |
| `cczj-transition-all` | `transition: all 0.2s ease;` |
| `cczj-transition-colors` | `transition: color 0.2s ease, background-color 0.2s ease, border-color 0.2s ease;` |
| `cczj-hover-underline`（条件类） | `text-decoration: underline;` |
| `cczj-hover-bg-accent-alpha`（条件类） | `background: var(--accent-alpha-10);` |
| `cczj-overflow-clip` | `overflow: clip;` |

