(function () {
  'use strict';

  if (window.__dazGameModalModuleLoaderActive) {
    return;
  }
  window.__dazGameModalModuleLoaderActive = true;

  const supportsModules = 'noModule' in HTMLScriptElement.prototype;
  const current = document.currentScript;
  const base = current && current.src ? current.src.replace(/[^/]+$/, '') : '';
  const moduleUrl = `${base}ui/bootstrap.mjs`;
  const MODULE_READY_TIMEOUT_MS = 2500;
  const ERROR_CLEAR_DELAY_MS = 4000;
  let hasError = false;

  function appendStatus(message, asError) {
    const status = document.getElementById('daz-game-modal-inline-status') || document.createElement('div');
    status.id = 'daz-game-modal-inline-status';
    status.textContent = message;
    status.style.cssText =
      'position:fixed;left:12px;bottom:12px;z-index:2147483647;background:' +
      (asError ? '#4a120f' : '#2d1810') +
      ';color:#d4af37;border:1px solid rgba(212,175,55,.45);padding:6px 10px;font:12px/1.3 Cinzel,Georgia,serif;max-width:40vw;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;box-shadow:0 0 12px rgba(0,0,0,.35);';
    (document.body || document.documentElement).appendChild(status);
    if (asError) {
      hasError = true;
      window.__dazGameModalLoadError = true;
    }
  }

  function clearStatus() {
    const status = document.getElementById('daz-game-modal-inline-status');
    if (status) {
      status.remove();
    }
  }

  if (!supportsModules) {
    hasError = true;
    window.__dazGameModalLoadError = true;
    appendStatus('daz game modal: module scripts unsupported in this browser', true);
    return;
  }

  const moduleScript = document.createElement('script');
  moduleScript.type = 'module';
  moduleScript.src = moduleUrl;
  moduleScript.onload = () => {
    if (!hasError && !window.__dazGameModalLoadError) {
      setTimeout(clearStatus, 1200);
    }
  };
  moduleScript.onerror = () => {
    hasError = true;
    window.__dazGameModalLoadError = true;
    appendStatus('daz game modal: module entry failed', true);
  };
  (document.head || document.documentElement).appendChild(moduleScript);

  window.__dazGameModalModuleLoading = true;
  setTimeout(() => {
    if (!window.__dazGameModalActive) {
      hasError = true;
      window.__dazGameModalLoadError = true;
      appendStatus('daz game modal: module bootstrap did not initialize', true);
    }
  }, MODULE_READY_TIMEOUT_MS);

  appendStatus('daz game modal: trying module bootstrap...');
  setTimeout(() => {
    if (!hasError && !window.__dazGameModalLoadError) {
      clearStatus();
    }
  }, ERROR_CLEAR_DELAY_MS);
})();
