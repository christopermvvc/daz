(function () {
  'use strict';

  if (window.__dazGameModalLoader) {
    return;
  }
  window.__dazGameModalLoader = true;

  const self = document.currentScript
    || Array.from(document.getElementsByTagName('script')).find((s) => /cytube-room-game-modal\.js/.test(s.src || ''));

  if (!self || !self.src) {
    console.error('[daz-game-modal] Loader requires a src-based script reference.');
    return;
  }

  const fullPath = new URL('cytube-room-game-modal.jss.js', self.src).href;
  if (window.__dazGameModalLoaded === fullPath) {
    return;
  }

  const script = document.createElement('script');
  script.src = fullPath;
  script.async = true;
  script.crossOrigin = 'anonymous';
  script.onload = function () {
    window.__dazGameModalLoaded = fullPath;
  };
  script.onerror = function () {
    console.error('[daz-game-modal] Failed to load external js bundle:', fullPath);
    window.__dazGameModalLoader = false;
  };

  (document.head || document.documentElement).appendChild(script);
})();
