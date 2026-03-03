import { showStatus, hideStatus } from './status.mjs';
import { loadLegacyModal, getLegacyUrl } from './legacy-loader.mjs';

window.__dazGameModalMode = 'module-bootstrap';
showStatus('daz game modal: module bootstrap initializing...');

function onFailure(message, error) {
  console.warn('[daz-game-modal] module bootstrap fallback to monolith:', message, error);
  return loadLegacyModal();
}

export async function start() {
  try {
    await loadLegacyModal();
    hideStatus();
    return;
  } catch (error) {
    hideStatus();
    onFailure('legacy load error', error);
  }
}

start().catch((error) => {
  onFailure('bootstrap failure', error);
  showStatus(`daz game modal: fallback load failed (${getLegacyUrl()})`, true);
});
