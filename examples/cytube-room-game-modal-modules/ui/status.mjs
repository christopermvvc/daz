const STATUS_ID = 'daz-game-modal-inline-status';
const DEFAULT_STATUS_STYLE =
  'position:fixed;left:12px;bottom:12px;z-index:2147483647;background:#2d1810;' +
  'color:#d4af37;border:1px solid rgba(212,175,55,.45);padding:6px 10px;' +
  'font:12px/1.3 Cinzel,Georgia,serif;max-width:40vw;white-space:nowrap;overflow:hidden;' +
  'text-overflow:ellipsis;box-shadow:0 0 12px rgba(0,0,0,.35);';

export function showStatus(message, asError) {
  const status = document.getElementById(STATUS_ID) || document.createElement('div');
  status.id = STATUS_ID;
  status.textContent = message;
  status.style.cssText = DEFAULT_STATUS_STYLE.replace(
    'background:#2d1810;',
    `background:${asError ? '#4a120f' : '#2d1810'};`,
  );
  (document.body || document.documentElement).appendChild(status);
}

export function hideStatus() {
  const status = document.getElementById(STATUS_ID);
  if (status) {
    status.remove();
  }
}
