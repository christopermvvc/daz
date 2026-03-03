(function () {
  'use strict';

  if (window.__dazGameModalLoader) {
    return;
  }
  window.__dazGameModalLoader = true;

  const self = document.currentScript
    || Array.from(document.getElementsByTagName('script')).find((s) => /cytube-room-game-modal\.js/.test(s.src || ''));

  const bundleHint = (self && self.dataset && self.dataset.bundle) || window.__dazGameModalBundleUrl || '';
  if (!self || !self.src) {
    if (!bundleHint) {
      console.error('[daz-game-modal] Loader needs a src-based script reference or a `data-bundle` value.');
      window.__dazGameModalLoader = false;
      return;
    }
  }

  const fullPath = bundleHint
    ? new URL(bundleHint, self && self.src ? self.src : window.location.href).href
    : new URL('cytube-room-game-modal.jss.js', self.src).href;
  if (window.__dazGameModalLoaded === fullPath) {
    return;
  }

  const script = document.createElement('script');
  script.src = fullPath;
  script.async = true;
  script.onload = function () {
    window.__dazGameModalLoaded = fullPath;
    console.info('[daz-game-modal] Loaded bundle:', fullPath);
  };
  script.onerror = function () {
    console.error('[daz-game-modal] Failed to load external js bundle:', fullPath);
    window.__dazGameModalLoader = false;
  };

  (document.head || document.documentElement).appendChild(script);
})();
