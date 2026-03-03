const legacyUrl = 'https://raw.githack.com/hildolfr/daz/master/examples/cytube-room-game-modal.js';
const LEGACY_SCRIPT_ID = 'daz-game-modal-legacy-script';

export function getLegacyUrl() {
  return legacyUrl;
}

export function loadLegacyModal() {
  return new Promise((resolve, reject) => {
    if (window.__dazGameModalActive) {
      resolve();
      return;
    }

    const existing = document.getElementById(LEGACY_SCRIPT_ID);
    if (existing) {
      existing.remove();
    }

    const script = document.createElement('script');
    script.id = LEGACY_SCRIPT_ID;
    script.async = true;
    script.src = legacyUrl;
    script.onload = () => resolve();
    script.onerror = () => reject(new Error('Legacy script failed to load'));
    (document.head || document.documentElement).appendChild(script);
  });
}
