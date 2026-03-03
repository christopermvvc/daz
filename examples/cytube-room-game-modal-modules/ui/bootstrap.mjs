import { hideStatus, showStatus } from './status.mjs';
import { loadLegacyBundle } from './legacy-loader.mjs';

window.__dazGameModalMode = 'module-bootstrap';
window.__dazGameModalLoadError = false;
window.__dazGameModalActive = false;
showStatus('daz game modal: loading full modal bundle...');

export async function start() {
  try {
    await loadLegacyBundle();
    showStatus('daz game modal: root mounted, ready');
    window.__dazGameModalActive = true;
    setTimeout(hideStatus, 1200);
    return;
  } catch (error) {
    window.__dazGameModalLoadError = true;
    window.__dazGameModalActive = false;
    console.warn('[daz-game-modal] module bootstrap failed:', error);
    showStatus(`daz game modal: module bootstrap failed (${error && error.message ? error.message : 'unknown'})`, true);
    throw error;
  }
}

start().catch(() => {});
