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

  function appendStatus(message, asError) {
    const status = document.getElementById('daz-game-modal-inline-status') || document.createElement('div');
    status.id = 'daz-game-modal-inline-status';
    status.textContent = message;
    status.style.cssText =
      'position:fixed;left:12px;bottom:12px;z-index:2147483647;background:' +
      (asError ? '#4a120f' : '#2d1810') +
      ';color:#d4af37;border:1px solid rgba(212,175,55,.45);padding:6px 10px;font:12px/1.3 Cinzel,Georgia,serif;max-width:40vw;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;box-shadow:0 0 12px rgba(0,0,0,.35);';
    (document.body || document.documentElement).appendChild(status);
  }

  function clearStatus() {
    const status = document.getElementById('daz-game-modal-inline-status');
    if (status) {
      status.remove();
    }
  }

  if (!supportsModules) {
    appendStatus('daz game modal: module scripts unsupported in this browser', true);
    return;
  }

  const moduleScript = document.createElement('script');
  moduleScript.type = 'module';
  moduleScript.src = moduleUrl;
  moduleScript.onload = () => {
    setTimeout(clearStatus, 1200);
  };
  moduleScript.onerror = () => {
    appendStatus('daz game modal: module entry failed', true);
    setTimeout(clearStatus, 4000);
  };
  (document.head || document.documentElement).appendChild(moduleScript);

  window.__dazGameModalModuleLoading = true;
  setTimeout(() => {
    if (!window.__dazGameModalActive) {
      appendStatus('daz game modal: module bootstrap did not initialize', true);
      setTimeout(clearStatus, 4000);
    }
  }, MODULE_READY_TIMEOUT_MS);

  appendStatus('daz game modal: trying module bootstrap...');
  setTimeout(clearStatus, 2500);
})();
