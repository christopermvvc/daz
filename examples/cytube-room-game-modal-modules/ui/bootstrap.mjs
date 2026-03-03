import { showStatus, hideStatus } from './status.mjs';

window.__dazGameModalMode = 'module-bootstrap';
window.__dazGameModalActive = true;
showStatus('daz game modal: module bootstrap initializing...');

export async function start() {
  showStatus('daz game modal: module bootstrap initialized (no fallback)', false);
  setTimeout(hideStatus, 1200);
}

start().catch((error) => {
  console.warn('[daz-game-modal] module bootstrap failed:', error);
  showStatus(`daz game modal: module bootstrap failed (${error && error.message ? error.message : 'unknown'})`, true);
});
