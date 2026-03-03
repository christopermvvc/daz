(function () {
  'use strict';

  if (window.__dazGameModalLoader) {
    return;
  }
  window.__dazGameModalLoader = true;
  const DEBUG = false;

  function log(message, ...args) {
    if (!DEBUG) {
      return;
    }
    console.info('[daz-game-modal]', message, ...args);
  }

  const self = document.currentScript
    || Array.from(document.getElementsByTagName('script')).find((s) => /cytube-room-game-modal\.js/.test(s.src || ''))
    || Array.from(document.getElementsByTagName('script')).slice(-1)[0];

  const explicitBundle = (self && self.dataset && self.dataset.bundle) || window.__dazGameModalBundleUrl;
  const base = self && self.src ? self.src : window.location.href;
  const candidates = [];
  if (explicitBundle) {
    candidates.push(explicitBundle);
  }
  if (self && self.src) {
    candidates.push('cytube-room-game-modal.jss.js');
  }
  if (!self || !self.src) {
    candidates.push('https://raw.githubusercontent.com/hildolfr/daz/master/examples/cytube-room-game-modal.jss.js');
    candidates.push('/examples/cytube-room-game-modal.jss.js');
    candidates.push('/cytube-room-game-modal.jss.js');
  }

  const seen = new Set();
  function createLoader(url) {
    const script = document.createElement('script');
    script.src = new URL(url, base).href;
    script.async = true;
    script.onload = function () {
      window.__dazGameModalLoaded = script.src;
      console.info('[daz-game-modal] Loaded bundle:', script.src);
    };
    script.onerror = function () {
      loadNextBundle();
    };
    (document.head || document.documentElement).appendChild(script);
  }

  function loadNextBundle() {
    if (!candidates.length) {
      console.error('[daz-game-modal] Failed to load bundle from all candidates:', Array.from(seen));
      window.__dazGameModalLoader = false;
      const failBanner = document.createElement('div');
      failBanner.textContent = 'daz modal failed to load';
      failBanner.style.cssText = 'position:fixed;left:12px;bottom:12px;z-index:2147483647;background:#2d1810;color:#d4af37;border:1px solid rgba(212,175,55,.45);padding:6px 10px;font:12px/1.3 Cinzel,Georgia,serif;';
      failBanner.id = 'daz-game-modal-load-fail';
      if (!document.getElementById('daz-game-modal-load-fail')) {
        (document.body || document.documentElement).appendChild(failBanner);
      }
      return;
    }

    const next = candidates.shift();
    if (!next || seen.has(next)) {
      return loadNextBundle();
    }
    seen.add(next);
    const normalized = new URL(next, base).href;
    log('candidate bundle url', normalized);
    if (window.__dazGameModalLoaded === normalized) {
      return;
    }
    createLoader(normalized);
  }

  if (window.__dazGameModalLoaded) {
    const expected = candidates.map((item) => new URL(item, base).href);
    if (expected.includes(window.__dazGameModalLoaded)) {
      return;
    }
  }

  loadNextBundle();
})();
