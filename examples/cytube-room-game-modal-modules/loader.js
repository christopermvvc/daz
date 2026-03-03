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
  const legacyUrl = `${base}../cytube-room-game-modal.js`;
  const MODULE_READY_TIMEOUT_MS = 6500;
  const MODULE_LOAD_TIMEOUT_MS = 5000;
  const ERROR_CLEAR_DELAY_MS = 4000;
  let hasError = false;

  function appendStatusAndError(message) {
    appendStatus(message, true);
    hasError = true;
    window.__dazGameModalLoadError = true;
  }

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

  function waitFor(
    predicate,
    timeoutMs,
    onTimeout,
  ) {
    const start = Date.now();
    const intervalMs = 120;
    return new Promise((resolve, reject) => {
      const timer = setInterval(() => {
        try {
          if (predicate()) {
            clearInterval(timer);
            resolve();
            return;
          }
        } catch (error) {
          clearInterval(timer);
          reject(error);
          return;
        }

        if (Date.now() - start >= timeoutMs) {
          clearInterval(timer);
          reject(new Error(onTimeout));
        }
      }, intervalMs);
    });
  }

  function loadScript(url, type) {
    return new Promise((resolve, reject) => {
      const script = document.createElement('script');
      script.type = type;
      script.src = url;
      script.async = true;
      script.addEventListener(
        'load',
        () => {
          resolve();
        },
        { once: true },
      );
      script.addEventListener(
        'error',
        () => reject(new Error(`daz game modal: script failed to load (${url})`)),
        { once: true },
      );
      (document.head || document.documentElement).appendChild(script);
    });
  }

  function loadModuleBootstrap() {
    appendStatus('daz game modal: trying module bootstrap...');
    return Promise.race([
      loadScript(moduleUrl, 'module'),
      waitFor(() => false, MODULE_LOAD_TIMEOUT_MS, `daz game modal: module script timeout (${moduleUrl})`),
    ]);
  }

  function loadLegacyFallback() {
    if (!legacyUrl) {
      throw new Error('daz game modal: unable to resolve legacy fallback URL');
    }
    appendStatus('daz game modal: using legacy fallback...');
    return Promise.race([
      loadScript(legacyUrl, 'text/javascript'),
      waitFor(() => false, MODULE_READY_TIMEOUT_MS, `daz game modal: legacy script timeout (${legacyUrl})`),
    ]);
  }

  function hasModalReady() {
    return !!document.getElementById('daz-game-modal-root') || !!window.__dazGameModalActive;
  }

  async function bootstrap() {
    if (!supportsModules) {
      appendStatus('daz game modal: module scripts unsupported in this browser, using legacy', true);
      try {
        await loadLegacyFallback();
        await waitFor(hasModalReady, MODULE_READY_TIMEOUT_MS, 'daz game modal: legacy fallback did not mount');
        clearStatus();
      } catch (error) {
        appendStatusAndError(
          `daz game modal: legacy fallback failed (${error && error.message ? error.message : 'unknown'})`,
        );
      }
      return;
    }

    try {
      await loadModuleBootstrap();
      await waitFor(
        () => {
          return hasModalReady() || !!window.__dazGameModalLoadError;
        },
        MODULE_READY_TIMEOUT_MS,
        'daz game modal: module bootstrap did not initialize',
      );
      if (window.__dazGameModalLoadError) {
        throw new Error('daz game modal: module bootstrap reported error');
      }
      if (hasModalReady()) {
        setTimeout(clearStatus, 1200);
        return;
      }
      throw new Error('daz game modal: no modal root after module bootstrap');
    } catch (moduleError) {
      appendStatus(`daz game modal: module bootstrap failed, fallback to legacy (${moduleError && moduleError.message ? moduleError.message : 'unknown'})`);
      try {
        await loadLegacyFallback();
        await waitFor(hasModalReady, MODULE_READY_TIMEOUT_MS, 'daz game modal: legacy fallback did not mount');
        setTimeout(clearStatus, 1200);
      } catch (legacyError) {
        appendStatusAndError(
          `daz game modal: legacy fallback failed (${legacyError && legacyError.message ? legacyError.message : 'unknown'})`,
        );
      }
    }
  }

  window.__dazGameModalModuleLoading = true;
  bootstrap().finally(() => {
    window.__dazGameModalModuleLoading = false;
    if (!hasError && !window.__dazGameModalLoadError && hasModalReady()) {
      setTimeout(clearStatus, ERROR_CLEAR_DELAY_MS);
    }
  });
})();
