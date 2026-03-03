(function () {
  'use strict';

  const LOADER_BUILD_ID = '94fb3ff22-command-rows-v4';
  const LOADER_SOURCE = (() => {
    const current = document.currentScript;
    return current && current.src ? current.src : 'inline-or-unknown';
  })();

  console.info('[daz-game-modal] loader start', { build: LOADER_BUILD_ID, source: LOADER_SOURCE });

  const existingRoot = document.getElementById('daz-game-modal-root');
  if (window.__dazGameModalActive && window.__dazGameModalBuild === LOADER_BUILD_ID && existingRoot) {
    console.info('[daz-game-modal] skipping bootstrap: same build already mounted', {
      build: LOADER_BUILD_ID,
      source: LOADER_SOURCE,
    });
    return;
  }

  const priorBuild = window.__dazGameModalBuild || null;
  if (priorBuild && priorBuild !== LOADER_BUILD_ID && existingRoot) {
    console.warn('[daz-game-modal] replacing older build', {
      previousBuild: priorBuild,
      nextBuild: LOADER_BUILD_ID,
    });
    existingRoot.remove();
  }

  window.__dazGameModalActive = true;
  window.__dazGameModalBuild = LOADER_BUILD_ID;

  window.__dazGameModalBuildMeta = {
    build: LOADER_BUILD_ID,
    source: LOADER_SOURCE,
    loadedAt: Date.now(),
  };

  const STORAGE_MODE_KEY = 'daz-cytube-game-modal-mode-v1';
  const STORAGE_BALANCE_KEY = 'daz-cytube-game-modal-balance-v1';
  const STORAGE_LEFT_KEY = 'daz-cytube-game-modal-left-v1';
  const STORAGE_TOP_KEY = 'daz-cytube-game-modal-top-v1';
  const STORAGE_WIDTH_KEY = 'daz-cytube-game-modal-width-v1';
  const STORAGE_HEIGHT_KEY = 'daz-cytube-game-modal-height-v1';
  const STORAGE_NEED_BLA_KEY = 'daz-cytube-game-modal-need-bladder-v1';
  const STORAGE_NEED_ALC_KEY = 'daz-cytube-game-modal-need-alcohol-v1';
  const STORAGE_NEED_WEED_KEY = 'daz-cytube-game-modal-need-weed-v1';
  const STORAGE_NEED_FOOD_KEY = 'daz-cytube-game-modal-need-food-v1';
  const STORAGE_NEED_LUST_KEY = 'daz-cytube-game-modal-need-lust-v1';
  const STORAGE_EFFECTS_KEY = 'daz-cytube-game-modal-effects-v1';
  const STORAGE_RESTORE_LEFT_KEY = 'daz-cytube-game-modal-restore-left-v1';
  const STORAGE_RESTORE_TOP_KEY = 'daz-cytube-game-modal-restore-top-v1';
  const STORAGE_RESTORE_WIDTH_KEY = 'daz-cytube-game-modal-restore-width-v1';
  const STORAGE_RESTORE_HEIGHT_KEY = 'daz-cytube-game-modal-restore-height-v1';
  const STORAGE_SECTION_COLLAPSE_KEY = 'daz-cytube-game-modal-section-collapsed-v1';
  const STORAGE_COMMAND_PANEL_KEY = 'daz-cytube-game-modal-command-panels-v1';
  const DEFAULT_EFFECTS = {
    buffs: {},
    debuffs: {},
  };
  const DEFAULT_BALANCE = 1250;
  const MIN_NEED = 0;
  const MAX_NEED = 100;
  const MIN_MODAL_WIDTH = 280;
  const MIN_MODAL_HEIGHT = 220;
  const MINIMIZED_HEIGHT = 38;
  const MINIMIZED_WIDTH = 220;
  const DEFAULT_MARGIN = 12;
  const MINIMIZED_ICON = '▢';
  const OPEN_ICON = '—';
  const STATE_SAVE_DEBOUNCE_MS = 250;

  const state = {
    mode: 'open',
    balance: DEFAULT_BALANCE,
    needs: {
      bladder: 55,
      alcohol: 40,
      weed: 35,
      food: 70,
      lust: 20,
    },
    effects: {
      buffs: {},
      debuffs: {},
    },
    left: null,
    top: null,
    width: null,
    height: null,
    restoreLeft: null,
    restoreTop: null,
    restoreWidth: null,
    restoreHeight: null,
    collapsedSections: {
      currency: false,
      needs: false,
      buffs: false,
    },
    commandPanels: {
      games: false,
      cmds: false,
      etc: false,
    },
  };

  function clamp(value, min, max) {
    return Math.max(min, Math.min(max, value));
  }

  function parseStoredNumber(value, fallback) {
    if (!value) {
      return fallback;
    }
    const parsed = parseFloat(value);
    return Number.isFinite(parsed) ? parsed : fallback;
  }

  function getViewportBounds() {
    return {
      width: Math.max(window.innerWidth, 320),
      height: Math.max(window.innerHeight, 240),
    };
  }

  function defaultGeometry() {
    const viewport = getViewportBounds();
    return {
      width: clamp(Math.round(viewport.width * 0.38), MIN_MODAL_WIDTH, 840),
      height: clamp(Math.round(viewport.height * 0.42), MIN_MODAL_HEIGHT, 480),
    };
  }

  function normalizeGeometry() {
    const viewport = getViewportBounds();
    const defaults = defaultGeometry();
    const baseWidth = Number.isFinite(state.width) && state.width > 0 ? state.width : defaults.width;
    const baseHeight = Number.isFinite(state.height) && state.height > 0 ? state.height : defaults.height;
    const widthCap = Math.max(MIN_MODAL_WIDTH, viewport.width - 16);
    const heightCap = Math.max(MIN_MODAL_HEIGHT, viewport.height - 16);
    const openHeight = clamp(baseHeight, MIN_MODAL_HEIGHT, heightCap);
    const width = clamp(baseWidth, MIN_MODAL_WIDTH, widthCap);
    const leftDefault = DEFAULT_MARGIN;
    const topDefault = viewport.height - openHeight - DEFAULT_MARGIN;
    const maxLeft = Math.max(DEFAULT_MARGIN, viewport.width - width - DEFAULT_MARGIN);
    const maxTop = Math.max(DEFAULT_MARGIN, viewport.height - openHeight - DEFAULT_MARGIN);
    const left = clamp(Number.isFinite(state.left) ? state.left : leftDefault, DEFAULT_MARGIN, maxLeft);
    const top = clamp(Number.isFinite(state.top) ? state.top : topDefault, DEFAULT_MARGIN, maxTop);
    return { width, height: openHeight, left, top };
  }

  function loadState() {
    try {
      const storedMode = window.localStorage.getItem(STORAGE_MODE_KEY);
      if (storedMode === 'open' || storedMode === 'minimized') {
        state.mode = storedMode;
      }

      const storedBalance = parseInt(window.localStorage.getItem(STORAGE_BALANCE_KEY), 10);
      if (!Number.isNaN(storedBalance)) {
        state.balance = storedBalance;
      }
      state.needs.bladder = clamp(
        parseStoredNumber(window.localStorage.getItem(STORAGE_NEED_BLA_KEY), state.needs.bladder),
        MIN_NEED,
        MAX_NEED,
      );
      state.needs.alcohol = clamp(
        parseStoredNumber(window.localStorage.getItem(STORAGE_NEED_ALC_KEY), state.needs.alcohol),
        MIN_NEED,
        MAX_NEED,
      );
      state.needs.weed = clamp(
        parseStoredNumber(window.localStorage.getItem(STORAGE_NEED_WEED_KEY), state.needs.weed),
        MIN_NEED,
        MAX_NEED,
      );
      state.needs.food = clamp(
        parseStoredNumber(window.localStorage.getItem(STORAGE_NEED_FOOD_KEY), state.needs.food),
        MIN_NEED,
        MAX_NEED,
      );
      state.needs.lust = clamp(
        parseStoredNumber(window.localStorage.getItem(STORAGE_NEED_LUST_KEY), state.needs.lust),
        MIN_NEED,
        MAX_NEED,
      );

      try {
        const storedEffectsRaw = window.localStorage.getItem(STORAGE_EFFECTS_KEY);
        if (storedEffectsRaw) {
          const parsed = JSON.parse(storedEffectsRaw);
          if (parsed && typeof parsed === 'object') {
            const buffs = parsed.buffs || {};
            const debuffs = parsed.debuffs || {};
            Object.keys(buffs).forEach((key) => {
              const value = parseStoredNumber(buffs[key], 0);
              if (Number.isFinite(value) && value !== 0) {
                state.effects.buffs[key] = value;
              }
            });
            Object.keys(debuffs).forEach((key) => {
              const value = parseStoredNumber(debuffs[key], 0);
              if (Number.isFinite(value) && value !== 0) {
                state.effects.debuffs[key] = value;
              }
            });
          }
        }
      } catch (err) {
        // Ignore malformed effect state.
      }

      state.left = parseStoredNumber(window.localStorage.getItem(STORAGE_LEFT_KEY), null);
      state.top = parseStoredNumber(window.localStorage.getItem(STORAGE_TOP_KEY), null);
      state.width = parseStoredNumber(window.localStorage.getItem(STORAGE_WIDTH_KEY), null);
      state.height = parseStoredNumber(window.localStorage.getItem(STORAGE_HEIGHT_KEY), null);
      state.restoreLeft = parseStoredNumber(window.localStorage.getItem(STORAGE_RESTORE_LEFT_KEY), null);
      state.restoreTop = parseStoredNumber(window.localStorage.getItem(STORAGE_RESTORE_TOP_KEY), null);
      state.restoreWidth = parseStoredNumber(window.localStorage.getItem(STORAGE_RESTORE_WIDTH_KEY), null);
      state.restoreHeight = parseStoredNumber(window.localStorage.getItem(STORAGE_RESTORE_HEIGHT_KEY), null);

      const storedSections = window.localStorage.getItem(STORAGE_SECTION_COLLAPSE_KEY);
      if (storedSections) {
        const parsedSections = JSON.parse(storedSections);
        if (parsedSections && typeof parsedSections === 'object') {
          Object.keys(state.collapsedSections).forEach((key) => {
            if (typeof parsedSections[key] === 'boolean') {
              state.collapsedSections[key] = parsedSections[key];
            }
          });
        }
      }

      const storedCommandPanels = window.localStorage.getItem(STORAGE_COMMAND_PANEL_KEY);
      if (storedCommandPanels) {
        const parsedPanels = JSON.parse(storedCommandPanels);
        if (parsedPanels && typeof parsedPanels === 'object') {
          Object.keys(state.commandPanels).forEach((key) => {
            if (typeof parsedPanels[key] === 'boolean') {
              state.commandPanels[key] = parsedPanels[key];
            }
          });
        }
      }
    } catch (err) {
      // localStorage unavailable in restricted contexts.
    }
  }

  function saveState() {
    try {
      window.localStorage.setItem(STORAGE_MODE_KEY, state.mode);
      window.localStorage.setItem(STORAGE_BALANCE_KEY, String(state.balance));
      window.localStorage.setItem(STORAGE_NEED_BLA_KEY, String(state.needs.bladder));
      window.localStorage.setItem(STORAGE_NEED_ALC_KEY, String(state.needs.alcohol));
      window.localStorage.setItem(STORAGE_NEED_WEED_KEY, String(state.needs.weed));
      window.localStorage.setItem(STORAGE_NEED_FOOD_KEY, String(state.needs.food));
      window.localStorage.setItem(STORAGE_NEED_LUST_KEY, String(state.needs.lust));
      window.localStorage.setItem(STORAGE_EFFECTS_KEY, JSON.stringify(state.effects));
      if (Number.isFinite(state.left)) {
        window.localStorage.setItem(STORAGE_LEFT_KEY, String(Math.round(state.left)));
      }
      if (Number.isFinite(state.top)) {
        window.localStorage.setItem(STORAGE_TOP_KEY, String(Math.round(state.top)));
      }
      if (Number.isFinite(state.width)) {
        window.localStorage.setItem(STORAGE_WIDTH_KEY, String(Math.round(state.width)));
      }
      if (Number.isFinite(state.height)) {
        window.localStorage.setItem(STORAGE_HEIGHT_KEY, String(Math.round(state.height)));
      }
      if (Number.isFinite(state.restoreLeft)) {
        window.localStorage.setItem(STORAGE_RESTORE_LEFT_KEY, String(Math.round(state.restoreLeft)));
      }
      if (Number.isFinite(state.restoreTop)) {
        window.localStorage.setItem(STORAGE_RESTORE_TOP_KEY, String(Math.round(state.restoreTop)));
      }
      if (Number.isFinite(state.restoreWidth)) {
        window.localStorage.setItem(STORAGE_RESTORE_WIDTH_KEY, String(Math.round(state.restoreWidth)));
      }
      if (Number.isFinite(state.restoreHeight)) {
        window.localStorage.setItem(STORAGE_RESTORE_HEIGHT_KEY, String(Math.round(state.restoreHeight)));
      }
      window.localStorage.setItem(STORAGE_SECTION_COLLAPSE_KEY, JSON.stringify(state.collapsedSections));
      window.localStorage.setItem(STORAGE_COMMAND_PANEL_KEY, JSON.stringify(state.commandPanels));
    } catch (err) {
      // ignore storage failures.
    }
  }

  function getUiModuleBase() {
    const known = (() => {
      const current = document.currentScript;
      if (current && current.src) {
        return current.src;
      }

      const matching = Array.from(document.querySelectorAll('script')).find((script) => {
        const src = script && script.src;
        return typeof src === 'string' && src.includes('cytube-room-game-modal.js');
      });
      return matching && matching.src ? matching.src : null;
    })();

    if (!known) {
      return '';
    }
    return known.substring(0, known.lastIndexOf('/') + 1);
  }

  const UI_BASE = getUiModuleBase();
  function withCacheBuster(url) {
    try {
      const urlObj = new URL(url, LOADER_SOURCE || document.baseURI);
      urlObj.searchParams.set('v', LOADER_BUILD_ID);
      return urlObj.toString();
    } catch (err) {
      return `${url}${url.includes('?') ? '&' : '?'}v=${encodeURIComponent(LOADER_BUILD_ID)}`;
    }
  }

  const UI_MODULE_FILES = [
    'cytube-room-game-modal-modules/legacy/daz-game-modal-view.js',
    'cytube-room-game-modal-modules/legacy/daz-game-modal-needs.js',
    'cytube-room-game-modal-modules/legacy/daz-game-modal-buffs.js',
  ].map((path) => withCacheBuster(`${UI_BASE || ''}${path}`));
  const uiModuleLoadPromises = new Map();
  let uiViewLoaded = false;

  function loadUiModule(url) {
    if (uiModuleLoadPromises.has(url)) {
      return uiModuleLoadPromises.get(url);
    }

    const promise = new Promise((resolve, reject) => {
      const existing = Array.from(document.querySelectorAll('script')).find((script) => script.src === url);
      if (existing) {
        if (existing.__dazGameModalModuleLoaded) {
          resolve();
          return;
        }
        if (existing.readyState === 'loaded' || existing.readyState === 'complete') {
          existing.__dazGameModalModuleLoaded = true;
          resolve();
          return;
        }
        existing.addEventListener(
          'load',
          () => {
            existing.__dazGameModalModuleLoaded = true;
            resolve();
          },
          { once: true },
        );
        existing.addEventListener(
          'error',
          () => reject(new Error(`daz game modal: ui module failed to load ${url}`)),
          { once: true },
        );
        return;
      }

      const script = document.createElement('script');
      script.type = 'text/javascript';
      script.src = url;
      script.async = false;
      script.dataset.dazGameModalModule = '1';
      script.addEventListener(
        'load',
        () => {
          script.__dazGameModalModuleLoaded = true;
          resolve();
        },
        { once: true },
      );
      script.addEventListener(
        'error',
        () => reject(new Error(`daz game modal: ui module failed to load ${url}`)),
        { once: true },
      );
      (document.head || document.documentElement).appendChild(script);
    });

    uiModuleLoadPromises.set(url, promise);
    return promise;
  }

  async function ensureUiModules() {
    for (const url of UI_MODULE_FILES) {
      await loadUiModule(url);
    }
    if (!window.__dazGameModalView || !window.__dazGameModalNeeds || !window.__dazGameModalBuffs) {
      throw new Error('daz game modal: ui modules did not initialize');
    }
    uiViewLoaded = true;
  }

  function cssText() {
    if (!uiViewLoaded || !window.__dazGameModalView || typeof window.__dazGameModalView.cssText !== 'function') {
      throw new Error('daz game modal: missing UI view module');
    }
    return window.__dazGameModalView.cssText();
  }

  function createMarkup() {
    if (!uiViewLoaded || !window.__dazGameModalView || typeof window.__dazGameModalView.createMarkup !== 'function') {
      throw new Error('daz game modal: missing UI view module');
    }
    return window.__dazGameModalView.createMarkup();
  }

  function updateModeUI() {
    const root = document.getElementById('daz-game-modal-root');
    const minToggle = document.getElementById('daz-game-modal-min-toggle');
    if (root) {
      root.classList.toggle('daz-state-minimized', state.mode === 'minimized');
      if (minToggle) {
        const isMinimized = state.mode === 'minimized';
        minToggle.textContent = isMinimized ? OPEN_ICON : MINIMIZED_ICON;
        minToggle.title = isMinimized ? 'Restore' : 'Minimize';
        minToggle.setAttribute('aria-label', minToggle.title);
      }
      applyGeometry();
    }
    saveState();
  }

  function applySectionCollapseState() {
    const root = document.getElementById('daz-game-modal-root');
    if (!root) {
      return;
    }

    const collapsed = state.collapsedSections || {};
    Object.keys(collapsed).forEach((key) => {
      const section = root.querySelector(`.daz-game-card[data-section="${key}"]`);
      if (!section) {
        return;
      }
      const isCollapsed = Boolean(collapsed[key]);
      const toggle = section.querySelector('[data-action="toggle-section"]');
      section.classList.toggle('is-collapsed', isCollapsed);
      section.setAttribute('aria-expanded', String(!isCollapsed));
      if (toggle) {
        toggle.textContent = isCollapsed ? '▸' : '▾';
        toggle.setAttribute('aria-expanded', String(!isCollapsed));
      }
      const content = section.querySelector('.daz-game-card-content');
      if (content) {
        content.setAttribute('aria-hidden', String(isCollapsed));
      }
    });
  }

  function setSectionCollapsed(sectionKey, isCollapsed) {
    const root = document.getElementById('daz-game-modal-root');
    if (!root || !state.collapsedSections || !Object.prototype.hasOwnProperty.call(state.collapsedSections, sectionKey)) {
      return;
    }

    const normalized = Boolean(isCollapsed);
    const section = root.querySelector(`.daz-game-card[data-section="${sectionKey}"]`);
    if (!section) {
      return;
    }

    const toggle = section.querySelector('[data-action="toggle-section"]');
    section.classList.toggle('is-collapsed', normalized);
    section.setAttribute('aria-expanded', String(!normalized));
    if (toggle) {
      toggle.textContent = normalized ? '▸' : '▾';
      toggle.setAttribute('aria-expanded', String(!normalized));
    }
    const content = section.querySelector('.daz-game-card-content');
    if (content) {
      content.setAttribute('aria-hidden', String(normalized));
    }
    state.collapsedSections[sectionKey] = normalized;
    saveState();
  }

  function applyCommandPanelState() {
    const root = document.getElementById('daz-game-modal-root');
    if (!root) {
      return;
    }

    const panelState = state.commandPanels || {};
    Object.keys(panelState).forEach((key) => {
      const panel = root.querySelector(`.daz-game-command-category[data-command-category="${key}"]`);
      if (!panel) {
        return;
      }
      const isOpen = Boolean(panelState[key]);
      const toggle = panel.querySelector('[data-action="toggle-command-category"]');
      const list = panel.querySelector('.daz-game-command-list');
      panel.classList.toggle('is-open', isOpen);
      panel.setAttribute('aria-expanded', String(isOpen));
      if (toggle) {
        const chevron = toggle.querySelector('[data-command-chevron]');
        if (chevron) {
          chevron.textContent = isOpen ? '▾' : '▸';
        } else {
          toggle.textContent = isOpen ? '▾' : '▸';
        }
        toggle.setAttribute('aria-expanded', String(isOpen));
      }
      if (list) {
        list.setAttribute('aria-hidden', String(!isOpen));
      }
    });
  }

  function setCommandPanelOpen(categoryKey, isOpen) {
    const root = document.getElementById('daz-game-modal-root');
    if (!root || !state.commandPanels || !Object.prototype.hasOwnProperty.call(state.commandPanels, categoryKey)) {
      return;
    }

    const panel = root.querySelector(`.daz-game-command-category[data-command-category="${categoryKey}"]`);
    if (!panel) {
      return;
    }

    const isPanelOpen = Boolean(isOpen);
    const toggle = panel.querySelector('[data-action="toggle-command-category"]');
    const list = panel.querySelector('.daz-game-command-list');
    panel.classList.toggle('is-open', isPanelOpen);
    panel.setAttribute('aria-expanded', String(isPanelOpen));
    if (toggle) {
      const chevron = toggle.querySelector('[data-command-chevron]');
      if (chevron) {
        chevron.textContent = isPanelOpen ? '▾' : '▸';
      } else {
        toggle.textContent = isPanelOpen ? '▾' : '▸';
      }
      toggle.setAttribute('aria-expanded', String(isPanelOpen));
    }
    if (list) {
      list.setAttribute('aria-hidden', String(!isPanelOpen));
    }
    state.commandPanels[categoryKey] = isPanelOpen;
    saveState();
  }

  function refreshBalance() {
    const balance = document.getElementById('daz-modal-balance');
    if (balance) {
      balance.textContent = String(state.balance);
    }
  }

  function refreshNeeds() {
    const needsRenderer = window.__dazGameModalNeeds;
    if (needsRenderer && typeof needsRenderer.refresh === 'function') {
      needsRenderer.refresh(state.needs);
      return;
    }

    Object.entries(state.needs).forEach(([key, rawValue]) => {
      const meter = document.getElementById(`daz-modal-need-${key}-meter`);
      if (!meter) {
        return;
      }
      const value = clamp(rawValue, MIN_NEED, MAX_NEED);
      const percent = Math.max(0, Math.round((value / MAX_NEED) * 100));
      meter.style.setProperty('--dazNeedFill', `${percent}%`);
      meter.setAttribute('aria-valuenow', String(percent));
      meter.setAttribute('aria-valuetext', `${value}`);
    });
  }

  function refreshEffects() {
    const effectsRenderer = window.__dazGameModalBuffs;
    if (effectsRenderer && typeof effectsRenderer.refresh === 'function') {
      effectsRenderer.refresh(state.effects);
      return;
    }

    const container = document.getElementById('daz-modal-buffs-list');
    if (!container) {
      return;
    }
    container.innerHTML = '';
    const effects = state.effects || {};
    const buffs = effects.buffs && typeof effects.buffs === 'object' ? effects.buffs : {};
    const debuffs = effects.debuffs && typeof effects.debuffs === 'object' ? effects.debuffs : {};
    const active = [];

    Object.keys(buffs).forEach((key) => {
      const value = parseFloat(buffs[key]);
      if (Number.isFinite(value) && value !== 0) {
        active.push({ type: 'buff' });
      }
    });
    Object.keys(debuffs).forEach((key) => {
      const value = parseFloat(debuffs[key]);
      if (Number.isFinite(value) && value !== 0) {
        active.push({ type: 'debuff' });
      }
    });

    if (!active.length) {
      const placeholder = document.createElement('span');
      placeholder.className = 'daz-game-empty-list-space';
      placeholder.textContent = '—';
      container.appendChild(placeholder);
      return;
    }

    active.forEach((entry) => {
      const node = document.createElement('span');
      node.className = 'daz-game-effect-dot';
      node.textContent = entry.type === 'buff' ? '✨' : '☠️';
      container.appendChild(node);
    });
  }

  function captureOpenGeometrySnapshot() {
    const normalized = normalizeGeometry();
    state.restoreLeft = normalized.left;
    state.restoreTop = normalized.top;
    state.restoreWidth = normalized.width;
    state.restoreHeight = normalized.height;
  }

  function restoreOpenGeometrySnapshot() {
    if (
      Number.isFinite(state.restoreLeft) &&
      Number.isFinite(state.restoreTop) &&
      Number.isFinite(state.restoreWidth) &&
      Number.isFinite(state.restoreHeight)
    ) {
      state.left = state.restoreLeft;
      state.top = state.restoreTop;
      state.width = state.restoreWidth;
      state.height = state.restoreHeight;
    }
  }

  function applyGeometry() {
    const root = document.getElementById('daz-game-modal-root');
    if (!root) {
      return;
    }
    if (!Number.isFinite(state.width) || !Number.isFinite(state.height) || state.width <= 0 || state.height <= 0) {
      const defaults = defaultGeometry();
      state.width = defaults.width;
      state.height = defaults.height;
    }

    const normalized = normalizeGeometry();
    const viewport = getViewportBounds();
    const isMinimized = state.mode === 'minimized';
    if (isMinimized) {
      const minimizedLeft = clamp(
        DEFAULT_MARGIN,
        DEFAULT_MARGIN,
        Math.max(DEFAULT_MARGIN, viewport.width - MINIMIZED_WIDTH - DEFAULT_MARGIN),
      );
      root.style.setProperty('position', 'fixed', 'important');
      root.style.setProperty('left', `${minimizedLeft}px`, 'important');
      root.style.setProperty('top', 'auto', 'important');
      root.style.setProperty('right', 'auto', 'important');
      root.style.setProperty('bottom', `${DEFAULT_MARGIN}px`, 'important');
      root.style.setProperty('width', `${MINIMIZED_WIDTH}px`, 'important');
      root.style.setProperty('height', `${MINIMIZED_HEIGHT}px`, 'important');
      root.style.setProperty('display', 'block', 'important');
      root.style.setProperty('visibility', 'visible', 'important');
      root.style.setProperty('z-index', '2147483647', 'important');
      root.style.setProperty('user-select', 'none', 'important');
      root.style.setProperty('pointer-events', 'auto', 'important');
      root.style.setProperty('transform', 'none', 'important');
      root.style.setProperty('margin', '0', 'important');
      persistStateSoon();
      return;
    }

    const heightForRender = normalized.height;
    const widthForRender = normalized.width;
    const maxTop = Math.max(DEFAULT_MARGIN, viewport.height - heightForRender - DEFAULT_MARGIN);
    const clampedLeft = clamp(normalized.left, DEFAULT_MARGIN, Math.max(DEFAULT_MARGIN, viewport.width - widthForRender - DEFAULT_MARGIN));
    const clampedTop = clamp(normalized.top, DEFAULT_MARGIN, maxTop);

    state.left = clampedLeft;
    state.top = clampedTop;
    state.height = normalized.height;
    state.width = normalized.width;

    root.style.setProperty('position', 'fixed', 'important');
    root.style.setProperty('left', `${state.left}px`, 'important');
    root.style.setProperty('right', 'auto', 'important');
    root.style.setProperty('top', `${state.top}px`, 'important');
    root.style.setProperty('bottom', 'auto', 'important');
    root.style.setProperty('width', `${widthForRender}px`, 'important');
    root.style.setProperty('height', `${heightForRender}px`, 'important');
    root.style.setProperty('display', 'block', 'important');
    root.style.setProperty('visibility', 'visible', 'important');
    root.style.setProperty('z-index', '2147483647', 'important');
    root.style.setProperty('user-select', 'none', 'important');
    root.style.setProperty('pointer-events', 'auto', 'important');
    root.style.setProperty('transform', 'none', 'important');
    root.style.setProperty('margin', '0', 'important');
    persistStateSoon();
  }

  function getPointerPoint(event) {
    if (event.touches && event.touches[0]) {
      return { x: event.touches[0].clientX, y: event.touches[0].clientY };
    }
    if (event.changedTouches && event.changedTouches[0]) {
      return {
        x: event.changedTouches[0].clientX,
        y: event.changedTouches[0].clientY,
      };
    }
    return { x: event.clientX, y: event.clientY };
  }

  function getEventTarget(event) {
    const target = event.target;
    return target && target.nodeType === 1 ? target : target && target.parentElement;
  }

  function setMode(nextMode) {
    if (nextMode !== 'open' && nextMode !== 'minimized') {
      return;
    }
    const wasMinimized = state.mode === 'minimized';
    const switchingToMinimized = nextMode === 'minimized' && !wasMinimized;
    const switchingToOpen = nextMode === 'open' && wasMinimized;

    if (switchingToMinimized) {
      captureOpenGeometrySnapshot();
    } else if (switchingToOpen) {
      restoreOpenGeometrySnapshot();
    }

    state.mode = nextMode;
    updateModeUI();
  }

  let activeInteraction = null;
  let statePersistTimer = null;

  function beginInteraction(event, type) {
    const root = document.getElementById('daz-game-modal-root');
    if (!root) {
      return;
    }
    const viewport = getViewportBounds();
    if (type === 'resize' && state.mode === 'minimized') {
      return;
    }
    if (type === 'drag' && !Number.isFinite(state.left)) {
      state.left = DEFAULT_MARGIN;
    }
    if (type === 'drag' && !Number.isFinite(state.top)) {
      const defaults = defaultGeometry();
      state.top = viewport.height - defaults.height - DEFAULT_MARGIN;
    }

    activeInteraction = {
      type,
      startX: getPointerPoint(event).x,
      startY: getPointerPoint(event).y,
      startLeft: Number.isFinite(state.left) ? state.left : DEFAULT_MARGIN,
      startTop: Number.isFinite(state.top) ? state.top : viewport.height - state.height - DEFAULT_MARGIN,
      startWidth: state.width,
      startHeight: state.height,
    };

    if (type === 'drag') {
      root.style.setProperty('cursor', 'grabbing', 'important');
    }

    document.addEventListener('mousemove', onInteractionMove);
    document.addEventListener('mouseup', onInteractionStop);
    document.addEventListener('mouseleave', onInteractionStop);
    document.addEventListener('touchmove', onInteractionMove, { passive: false });
    document.addEventListener('touchcancel', onInteractionStop);
    document.addEventListener('touchend', onInteractionStop);
    event.preventDefault();
  }

  function onInteractionMove(event) {
    if (!activeInteraction) {
      return;
    }
    const source = event.touches && event.touches[0]
      ? event.touches[0]
      : event.changedTouches && event.changedTouches[0]
      ? event.changedTouches[0]
      : event;
    const dx = source.clientX - activeInteraction.startX;
    const dy = source.clientY - activeInteraction.startY;

    if (activeInteraction.type === 'drag') {
      state.left = activeInteraction.startLeft + dx;
      state.top = activeInteraction.startTop + dy;
    } else if (activeInteraction.type === 'resize' && state.mode !== 'minimized') {
      state.width = activeInteraction.startWidth + dx;
      state.height = activeInteraction.startHeight + dy;
    }

    applyGeometry();
    event.preventDefault();
  }

  function onInteractionStop() {
    if (!activeInteraction) {
      return;
    }
    activeInteraction = null;
    saveState();
    const root = document.getElementById('daz-game-modal-root');
    if (root) {
      root.style.setProperty('cursor', '', 'important');
    }
    document.removeEventListener('mousemove', onInteractionMove);
    document.removeEventListener('mouseup', onInteractionStop);
    document.removeEventListener('mouseleave', onInteractionStop);
    document.removeEventListener('touchmove', onInteractionMove);
    document.removeEventListener('touchcancel', onInteractionStop);
    document.removeEventListener('touchend', onInteractionStop);
  }

  function persistStateSoon() {
    if (statePersistTimer) {
      window.clearTimeout(statePersistTimer);
      statePersistTimer = null;
    }

    statePersistTimer = window.setTimeout(() => {
      statePersistTimer = null;
      saveState();
    }, STATE_SAVE_DEBOUNCE_MS);
  }

  function flushStateNow() {
    if (statePersistTimer) {
      window.clearTimeout(statePersistTimer);
      statePersistTimer = null;
    }
    saveState();
  }

  function onMouseDown(event) {
    const isMouse = event.type === 'mousedown';
    if (isMouse && event.button !== 0) {
      return;
    }
    const root = document.getElementById('daz-game-modal-root');
    if (!root) {
      return;
    }
    const target = getEventTarget(event);
    if (!target) {
      return;
    }
    const actionTarget = target.closest('[data-action]');
    if (actionTarget) {
      return;
    }
    if (target.closest('#daz-game-modal-resize-handle')) {
      beginInteraction(event, 'resize');
      return;
    }
    if (target.closest('#daz-game-modal-header')) {
      beginInteraction(event, 'drag');
    }
  }

  function onClick(event) {
    const target = getEventTarget(event);
    if (!target) {
      return;
    }
    const actionTarget = target.closest('[data-action]');
    if (actionTarget) {
      const action = actionTarget.getAttribute('data-action');
      if (action === 'toggle-min') {
        event.preventDefault();
        event.stopPropagation();
        setMode(state.mode === 'minimized' ? 'open' : 'minimized');
        return;
      }
      if (action === 'toggle-section') {
        event.preventDefault();
        event.stopPropagation();
        const section = actionTarget.getAttribute('data-section-key');
        if (section && state.collapsedSections && Object.prototype.hasOwnProperty.call(state.collapsedSections, section)) {
          setSectionCollapsed(section, !state.collapsedSections[section]);
        }
        return;
      }
      if (action === 'toggle-command-category') {
        event.preventDefault();
        event.stopPropagation();
        const category = actionTarget.getAttribute('data-command-category');
        if (category && state.commandPanels && Object.prototype.hasOwnProperty.call(state.commandPanels, category)) {
          setCommandPanelOpen(category, !state.commandPanels[category]);
        }
        return;
      }
      return;
    }
  }

  function bindEvents() {
    const root = document.getElementById('daz-game-modal-root');
    if (!root) return;
    root.addEventListener('click', onClick, true);
    root.addEventListener('mousedown', onMouseDown);
    root.addEventListener('touchstart', onMouseDown);
    window.addEventListener('resize', applyGeometry);
    window.addEventListener('orientationchange', applyGeometry);
    window.addEventListener('pagehide', flushStateNow);
    window.addEventListener('beforeunload', flushStateNow);
  }

  function maybeInjectStyles() {
    if (document.getElementById('daz-game-modal-inline-styles')) {
      return;
    }

    const style = document.createElement('style');
    style.id = 'daz-game-modal-inline-styles';
    style.textContent = cssText();
    (document.head || document.documentElement).appendChild(style);
  }

  function mount() {
    if (document.getElementById('daz-game-modal-root')) {
      return;
    }

    const root = document.createElement('div');
    root.id = 'daz-game-modal-root';
    root.dataset.dazGameModalBuild = LOADER_BUILD_ID;
    root.dataset.dazGameModalSource = LOADER_SOURCE;
    root.innerHTML = createMarkup();

    maybeInjectStyles();
    const container = document.body || document.documentElement;
    container.appendChild(root);
    applyGeometry();
    console.info('[daz-game-modal] mounted', { rootId: root.id });
    bindEvents();

    refreshBalance();
    refreshNeeds();
    refreshEffects();
    applySectionCollapseState();
    applyCommandPanelState();
    updateModeUI();
    saveState();
  }

  async function bootstrap() {
    try {
      await ensureUiModules();
      loadState();
      if (document.body) {
        mount();
      } else {
        window.addEventListener('DOMContentLoaded', mount, { once: true });
      }
      const activeRoot = document.getElementById('daz-game-modal-root');
      console.info('[daz-game-modal] bundle loaded', {
        rootExists: !!activeRoot,
        build: LOADER_BUILD_ID,
        source: LOADER_SOURCE,
        uiModules: UI_MODULE_FILES,
      });
    } catch (err) {
      const message = `${LOADER_BUILD_ID} failed: ${(err && err.message) || 'unknown'}`;
      console.error('[daz-game-modal] Failed to mount modal:', message, err);
      window.__dazGameModalLoadError = true;
      throw err;
    }
  }

  bootstrap();
})();
