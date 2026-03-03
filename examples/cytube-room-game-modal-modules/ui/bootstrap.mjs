import { hideStatus, showStatus } from './status.mjs';
import { loadLegacyBundle } from './legacy-loader.mjs';

window.__dazGameModalMode = 'module-bootstrap';
window.__dazGameModalLoadError = false;
window.__dazGameModalActive = false;
showStatus('daz game modal: loading full modal bundle...');

export async function start() {
  try {
    await loadLegacyBundle();
    if (!document.getElementById('daz-game-modal-root')) {
      throw new Error('daz game modal: legacy bundle loaded but root did not mount');
    }
    window.__dazGameModalActive = true;
    showStatus('daz game modal: modal initialized');
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
