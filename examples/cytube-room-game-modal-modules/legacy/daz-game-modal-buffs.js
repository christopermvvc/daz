(function () {
  'use strict';

  const DEFAULT_EFFECTS = {
    buffs: {},
    debuffs: {},
  };

  function refresh(effects) {
    const container = document.getElementById('daz-modal-buffs-list');
    if (!container) {
      return;
    }

    const currentEffects = effects && typeof effects === 'object'
      ? effects
      : DEFAULT_EFFECTS;
    const buffs = currentEffects.buffs && typeof currentEffects.buffs === 'object'
      ? currentEffects.buffs
      : {};
    const debuffs = currentEffects.debuffs && typeof currentEffects.debuffs === 'object'
      ? currentEffects.debuffs
      : {};

    container.innerHTML = '';
    const active = [];

    Object.keys(buffs).forEach((key) => {
      const value = Number.parseFloat(buffs[key]);
      if (Number.isFinite(value) && value !== 0) {
        active.push('buff');
      }
      return;
    });

    Object.keys(debuffs).forEach((key) => {
      const value = Number.parseFloat(debuffs[key]);
      if (Number.isFinite(value) && value !== 0) {
        active.push('debuff');
      }
      return;
    });

    if (!active.length) {
      const placeholder = document.createElement('span');
      placeholder.className = 'daz-game-empty-list-space';
      placeholder.textContent = '—';
      container.appendChild(placeholder);
      return;
    }

    active.forEach((type) => {
      const el = document.createElement('span');
      el.className = 'daz-game-effect-dot';
      el.textContent = type === 'buff' ? '✨' : '☠️';
      container.appendChild(el);
    });
  }

  window.__dazGameModalBuffs = {
    refresh,
    DEFAULT_EFFECTS,
  };
})();
