(function () {
  'use strict';

  if (window.__dazGameModalActive || document.getElementById('daz-game-modal-root')) {
    return;
  }

  function appendStatus(message, asError) {
    const status = document.getElementById('daz-game-modal-inline-status') || document.createElement('div');
    status.id = 'daz-game-modal-inline-status';
    status.textContent = message;
    status.style.cssText =
      'position:fixed;left:12px;bottom:12px;z-index:2147483647;background:' +
      (asError ? '#4a120f' : '#2d1810') +
      ';color:#d4af37;border:1px solid rgba(212,175,55,.45);padding:6px 10px;' +
      'font:12px/1.3 Cinzel,Georgia,serif;max-width:40vw;white-space:nowrap;overflow:hidden;' +
      'text-overflow:ellipsis;box-shadow:0 0 12px rgba(0,0,0,.35);';
    (document.body || document.documentElement).appendChild(status);
  }

  try {
    const current = document.currentScript && document.currentScript.src ? document.currentScript.src : '';
    const base = current ? current.replace(/[^/]+$/, '') : '';
    const targetUrl = `${base}../cytube-room-game-modal.js`;
    if (!base) {
      appendStatus('daz game modal: failed to resolve legacy fallback URL', true);
      window.__dazGameModalLoadError = true;
      return;
    }

    const fallbackScript = document.createElement('script');
    fallbackScript.src = targetUrl;
    fallbackScript.type = 'text/javascript';
    fallbackScript.referrerPolicy = 'no-referrer-when-downgrade';
    fallbackScript.addEventListener(
      'error',
      () => {
        appendStatus('daz game modal: legacy fallback failed to load', true);
        window.__dazGameModalLoadError = true;
        window.__dazGameModalBootstrapFailure = {
          reason: 'module-legacy-shim-load-error',
          legacyShimSrc: current,
          targetUrl,
          at: Date.now(),
        };
        console.error('[daz-game-modal] legacy shim failed to load', targetUrl);
      },
      { once: true },
    );
    (document.head || document.documentElement).appendChild(fallbackScript);
  } catch (error) {
    window.__dazGameModalLoadError = true;
    window.__dazGameModalBootstrapFailure = {
      reason: error && error.message ? error.message : 'unknown',
      at: Date.now(),
    };
    console.error('[daz-game-modal] legacy shim threw', error);
    appendStatus('daz game-modal: legacy shim execution error', true);
  }
})();
