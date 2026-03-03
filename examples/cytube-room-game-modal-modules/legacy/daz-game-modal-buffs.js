(function () {
  'use strict';

  const DEFAULT_EFFECTS = {
    buffs: {
      focus: 0,
      vigor: 0,
      luck: 0,
    },
    debuffs: {
      hangover: -10,
      nausea: -5,
      dizzy: -8,
    },
  };

  const BUFFS = [
    { section: 'buffs', key: 'focus' },
    { section: 'buffs', key: 'vigor' },
    { section: 'buffs', key: 'luck' },
    { section: 'debuffs', key: 'hangover' },
    { section: 'debuffs', key: 'nausea' },
    { section: 'debuffs', key: 'dizzy' },
  ];

  function formatEffect(value) {
    const parsed = Number.parseFloat(value);
    if (!Number.isFinite(parsed)) {
      return '0';
    }
    const rounded = Math.round(parsed);
    return rounded > 0 ? `+${rounded}` : `${rounded}`;
  }

  function refresh(effects) {
    const currentEffects = effects && typeof effects === 'object'
      ? effects
      : DEFAULT_EFFECTS;

    BUFFS.forEach((entry) => {
      const container = `${entry.section.slice(0, -1)}`;
      const source = currentEffects[container] || {};
      const el = document.getElementById(`daz-modal-${entry.section.slice(0, -1)}-${entry.key}`);
      if (!el) {
        return;
      }
      el.textContent = formatEffect(source[entry.key] === undefined ? DEFAULT_EFFECTS[container][entry.key] : source[entry.key]);
    });
  }

  window.__dazGameModalBuffs = {
    refresh,
    DEFAULT_EFFECTS,
  };
})();
