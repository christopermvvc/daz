const LEGACY_SCRIPT_ID = 'daz-game-modal-legacy-script';
const legacyUrl = new URL('../../cytube-room-game-modal.js', import.meta.url).href;
const MODAL_ROOT_ID = 'daz-game-modal-root';
const MODAL_MOUNT_TIMEOUT_MS = 6000;

function findLegacyScript() {
  return document.getElementById(LEGACY_SCRIPT_ID);
}

function waitForScriptLoad(script) {
  if (script.__dazGameModalLoaded) {
    return Promise.resolve();
  }

  if (script.readyState === 'loaded' || script.readyState === 'complete') {
    script.__dazGameModalLoaded = true;
    return Promise.resolve();
  }

  return new Promise((resolve, reject) => {
    const done = () => {
      script.__dazGameModalLoaded = true;
      resolve();
    };
    const fail = () => reject(new Error('daz game modal: legacy bundle script failed'));
    script.addEventListener('load', done, { once: true });
    script.addEventListener('error', fail, { once: true });
  });
}

function waitForRootMount(timeoutMs = MODAL_MOUNT_TIMEOUT_MS) {
  if (document.getElementById(MODAL_ROOT_ID)) {
    return Promise.resolve();
  }

  return new Promise((resolve, reject) => {
    const end = Date.now() + timeoutMs;
    const intervalMs = 100;
    const timer = setInterval(() => {
      if (document.getElementById(MODAL_ROOT_ID)) {
        clearInterval(timer);
        resolve();
      } else if (Date.now() >= end) {
        clearInterval(timer);
        reject(new Error('daz game modal: legacy root failed to mount'));
      }
    }, intervalMs);
  });
}

export function isLegacyLoaded() {
  return !!findLegacyScript() || !!window.__dazGameModalActive;
}

export function getLegacyLoadState() {
  const legacyScript = findLegacyScript();
  return {
    hasLegacyScript: !!legacyScript,
    legacyScriptSrc: legacyScript && legacyScript.src,
    legacyScriptReadyState: legacyScript && legacyScript.readyState,
    legacyScriptLoaded: legacyScript && legacyScript.__dazGameModalLoaded,
    hasRoot: !!document.getElementById(MODAL_ROOT_ID),
    hasWindowActive: !!window.__dazGameModalActive,
  };
}

export async function loadLegacyBundle() {
  if (isLegacyLoaded() && document.getElementById(MODAL_ROOT_ID)) {
    return;
  }

  const existing = findLegacyScript();
  let script = existing;
  if (existing) {
    await waitForScriptLoad(script);
    await waitForRootMount();
    return;
  }

  script = document.createElement('script');
  script.id = LEGACY_SCRIPT_ID;
  script.src = legacyUrl;
  script.type = 'text/javascript';
  script.referrerPolicy = 'no-referrer-when-downgrade';
  (document.head || document.documentElement).appendChild(script);
  await waitForScriptLoad(script);
  await waitForRootMount();
}
