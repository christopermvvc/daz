const LEGACY_SCRIPT_ID = 'daz-game-modal-legacy-script';
const legacyUrl = new URL('../cytube-room-game-modal.js', import.meta.url).href;

function findLegacyScript() {
  return document.getElementById(LEGACY_SCRIPT_ID);
}

export function isLegacyLoaded() {
  return !!findLegacyScript() || !!window.__dazGameModalActive;
}

export function loadLegacyBundle() {
  if (isLegacyLoaded() && document.getElementById('daz-game-modal-root')) {
    return Promise.resolve();
  }

  const existing = findLegacyScript();
  if (existing) {
    return new Promise((resolve, reject) => {
      if (document.getElementById('daz-game-modal-root')) {
        resolve();
        return;
      }
      existing.addEventListener('load', resolve, { once: true });
      existing.addEventListener('error', () => reject(new Error('daz game modal: legacy script load error')), { once: true });
    });
  }

  return new Promise((resolve, reject) => {
    const script = document.createElement('script');
    script.id = LEGACY_SCRIPT_ID;
    script.src = legacyUrl;
    script.type = 'text/javascript';
    script.referrerPolicy = 'no-referrer-when-downgrade';
    script.addEventListener(
      'load',
      () => resolve(),
      { once: true },
    );
    script.addEventListener(
      'error',
      () => reject(new Error('daz game modal: legacy bundle script failed')),
      { once: true },
    );
    (document.head || document.documentElement).appendChild(script);
  });
}
