(function () {
  'use strict';

  const MIN_NEED = 0;
  const MAX_NEED = 100;
  const NEED_SEGMENTS = 8;

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
      const meter = document.getElementById(`daz-modal-need-${key}-meter`);
      if (meter) {
        const segments = Math.round((value / MAX_NEED) * NEED_SEGMENTS);
        const filled = Math.max(0, Math.min(NEED_SEGMENTS, segments));
        meter.textContent = `${'🟢'.repeat(filled)}${'⚪'.repeat(Math.max(0, NEED_SEGMENTS - filled))}`;
      }
    });
  }

  window.__dazGameModalNeeds = {
    refresh,
    clampNeed,
  };
})();
