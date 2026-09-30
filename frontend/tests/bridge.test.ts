import assert from 'node:assert/strict';
import { afterEach, beforeEach, test } from 'node:test';
import { loadBridge, openExternal } from '../src/bridge.ts';

type Script = { src: string; async: boolean; onload: (() => void) | null; onerror: (() => void) | null; remove: () => void };
const originals = new Map<string, PropertyDescriptor | undefined>();
let scripts: Script[];
let timers: Map<number, () => void>;
let removed: Script[];
let external: unknown[][];

beforeEach(() => {
  scripts = []; removed = []; external = []; timers = new Map();
  let nextTimer = 0;
  const fakeWindow = {
    setTimeout(callback: () => void) { timers.set(++nextTimer, callback); return nextTimer; },
    clearTimeout(id: number) { timers.delete(id); },
    open(...args: unknown[]) { external.push(args); },
  };
  const fakeDocument = {
    createElement() {
      const script: Script = { src: '', async: false, onload: null, onerror: null, remove() { removed.push(script); } };
      return script;
    },
    head: { append(script: Script) { scripts.push(script); } },
  };
  for (const [name, value] of Object.entries({ window: fakeWindow, document: fakeDocument })) {
    originals.set(name, Object.getOwnPropertyDescriptor(globalThis, name));
    Object.defineProperty(globalThis, name, { value, configurable: true });
  }
});
afterEach(() => {
  for (const [name, descriptor] of originals) {
    if (descriptor) Object.defineProperty(globalThis, name, descriptor);
    else Reflect.deleteProperty(globalThis, name);
  }
});

test('existing MAX SDK does not load another script', async () => {
  window.WebApp = { initData: 'synthetic' };
  await loadBridge();
  assert.equal(scripts.length, 0);
});
test('concurrent callers share a single SDK load', async () => {
  const first = loadBridge();
  assert.equal(loadBridge(), first);
  assert.equal(scripts.length, 1);
  assert.equal(scripts[0].src, 'https://st.max.ru/js/max-web-app.js');
  window.WebApp = { initData: 'synthetic' };
  scripts[0].onload?.();
  await first;
  assert.equal(timers.size, 0);
});
test('network failure removes the script and allows a clean retry', async () => {
  const first = loadBridge();
  scripts[0].onerror?.();
  await assert.rejects(first, /NO_MAX/);
  assert.deepEqual(removed, [scripts[0]]);
  assert.equal(timers.size, 0);
  const retry = loadBridge();
  assert.equal(scripts.length, 2);
  window.WebApp = { initData: 'synthetic' };
  scripts[1].onload?.();
  await retry;
});
test('a script load without WebApp is not treated as SDK success', async () => {
  const pending = loadBridge();
  scripts[0].onload?.();
  await assert.rejects(pending, /NO_MAX/);
  assert.equal(removed.length, 1);
});
test('timeout cleans handlers and a late event cannot settle the next attempt', async () => {
  const first = loadBridge();
  const lateLoad = scripts[0].onload;
  [...timers.values()][0]();
  await assert.rejects(first, /NO_MAX/);
  assert.equal(scripts[0].onload, null);
  const retry = loadBridge();
  window.WebApp = { initData: 'synthetic' };
  lateLoad?.();
  scripts[1].onload?.();
  await retry;
  assert.equal(timers.size, 0);
});
test('external links use MAX when available and reject unsafe protocols', () => {
  const links: string[] = [];
  window.WebApp = { initData: 'synthetic', openLink: url => { links.push(url); } };
  openExternal('https://example.test/course');
  assert.deepEqual(links, ['https://example.test/course']);
  assert.equal(external.length, 0);
  for (const url of ['javascript:alert(1)', 'http://example.test', 'data:text/html,test']) {
    assert.throws(() => openExternal(url), /Unsafe URL/);
  }
});
test('browser fallback isolates the new window from the application', () => {
  openExternal('https://example.test/course');
  assert.deepEqual(external, [['https://example.test/course', '_blank', 'noopener,noreferrer']]);
});
