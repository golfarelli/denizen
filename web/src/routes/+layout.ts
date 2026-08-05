// This is a pure SPA served from a static Go binary (see vite.config.ts) —
// there is no server at runtime to render on, so every route is
// client-rendered and none are prerendered at build time.
export const ssr = false;
export const prerender = false;
