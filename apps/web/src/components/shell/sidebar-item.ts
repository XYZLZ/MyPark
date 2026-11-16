/**
 * One sidebar row (mockup 1a): 48 px tall, so above the 44 px floor. The focus ring is white with a
 * gap, because a navy ring would vanish on the navy sidebar (white on navy is well above 3:1).
 */
export const SIDEBAR_ITEM_CLASS =
  'flex min-h-12 w-full items-center gap-3.5 rounded-xl px-4 text-left text-base font-medium text-brand-primary-contrast no-underline outline-none hover:bg-white/10 focus-visible:outline-3 focus-visible:outline-offset-2 focus-visible:outline-white';

export const SIDEBAR_ITEM_CURRENT_CLASS = 'bg-nav-active font-bold shadow-[0_6px_14px_rgba(0,0,0,0.18)] hover:bg-nav-active';
