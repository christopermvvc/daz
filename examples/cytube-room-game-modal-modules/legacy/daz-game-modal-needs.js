(function () {
  'use strict';

  const MIN_NEED = 0;
  const MAX_NEED = 100;

  function clampNeed(value) {
    const parsed = Number.parseInt(value, 10);
    if (!Number.isFinite(parsed)) {
      return 0;
    }
    return Math.max(MIN_NEED, Math.min(MAX_NEED, parsed));
  }

  function refresh(needs) {
    const keys = ['bladder', 'alcohol', 'weed', 'food', 'lust'];
    keys.forEach((key) => {
      const value = clampNeed(needs && needs[key]);
      const label = document.getElementById(`daz-modal-need-${key}`);
      const bar = document.getElementById(`daz-modal-need-${key}-bar`);
      if (label) {
        label.textContent = `${value}`;
      }
      if (bar) {
        bar.style.width = `${value}%`;
      }
    });
  }

  window.__dazGameModalNeeds = {
    refresh,
    clampNeed,
  };
})();
