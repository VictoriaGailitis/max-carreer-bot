declare global {
  interface Window { WebApp?: { initData: string; ready?: () => void; openLink?: (url: string) => void } }
}
let bridge: Promise<void> | undefined;
export function loadBridge(): Promise<void> {
  if (window.WebApp) return Promise.resolve();
  return bridge ??= new Promise<void>((resolve, reject) => {
    const script = document.createElement('script');
    let settled = false;
    const finish = (success: boolean) => {
      if (settled) return;
      settled = true;
      window.clearTimeout(timer);
      script.onload = null;
      script.onerror = null;
      if (success) resolve();
      else { script.remove(); reject(new Error('NO_MAX')); }
    };
    const timer = window.setTimeout(() => finish(false), 8000);
    script.src = 'https://st.max.ru/js/max-web-app.js';
    script.async = true;
    script.onload = () => finish(!!window.WebApp);
    script.onerror = () => finish(false);
    try { document.head.append(script); } catch { finish(false); }
  }).finally(() => { bridge = undefined; });
}
export function openExternal(url: string) {
  const target = new URL(url);
  if (target.protocol !== 'https:') throw new Error('Unsafe URL');
  if (window.WebApp?.initData && window.WebApp.openLink) window.WebApp.openLink(target.href);
  else window.open(target.href, '_blank', 'noopener,noreferrer');
}
