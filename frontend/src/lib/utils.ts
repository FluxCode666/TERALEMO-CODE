import { clsx, type ClassValue } from 'clsx'
import { extendTailwindMerge } from 'tailwind-merge'

const twMerge = extendTailwindMerge({
  extend: {
    classGroups: {
      rounded: [{ rounded: ['ui-sm', 'ui-md', 'ui-lg', 'ui-xl'] }],
      shadow: [{ shadow: ['card', 'card-hover', 'glass', 'glass-sm', 'glow', 'glow-lg', 'inner-glow'] }],
    },
  },
})

/** 合并条件类名，并让调用方的 Tailwind 类覆盖组件默认值（Tailwind 3）。 */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs))
}
