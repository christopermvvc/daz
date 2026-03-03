(function () {
  'use strict';

  const LOADER_BUILD_ID = '8849a35-live-balance-v2';
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
  if (priorBuild && priorBuild !== LOADER_BUILD_ID) {
    if (typeof window.__dazGameModalCleanup === 'function') {
      try {
        window.__dazGameModalCleanup({ removeRoot: true, reason: 'build-replace' });
      } catch (err) {
        // Ignore stale cleanup failures and continue with replacement.
      }
    }

    console.warn('[daz-game-modal] replacing older build', {
      previousBuild: priorBuild,
      nextBuild: LOADER_BUILD_ID,
    });
    const staleRoot = document.getElementById('daz-game-modal-root');
    if (staleRoot) {
      staleRoot.remove();
    }
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
  const WS_TOKEN_STORAGE_KEY = 'daz-wsbridge-token-v1';
  const WS_URL_STORAGE_KEY = 'daz-wsbridge-url-v1';
  const WS_QUERY_TOKEN_KEY = 'daz_ws_token';
  const WS_QUERY_URL_KEY = 'daz_ws_url';
  const WS_QUERY_HOST_KEY = 'daz_ws_host';
  const WS_RUNTIME_HOST_KEY = '__DAZ_WSBRIDGE_HOST';
  const WS_DEFAULT_PORT = '8091';
  const WS_DEFAULT_PATH = '/ws';
  const WS_REQUEST_TIMEOUT_MS = 7000;
  const WS_BALANCE_REFRESH_MS = 30000;

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

  const wsState = {
    socket: null,
    connected: false,
    reconnectTimer: null,
    reconnectAttempts: 0,
    suppressReconnect: false,
    requestCounter: 0,
    pending: new Map(),
    welcome: null,
    periodicRefreshTimer: null,
    balanceInFlight: false,
  };

  let boundRoot = null;
  let isDestroyed = false;

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
    const balanceNode = document.getElementById('daz-modal-balance');
    if (!balanceNode) {
      return;
    }

    const numericBalance = Number(state.balance);
    const safeBalance = Number.isFinite(numericBalance) ? Math.round(numericBalance) : 0;
    state.balance = safeBalance;
    balanceNode.textContent = `$${safeBalance.toLocaleString('en-US')}`;
  }

  function setCurrencyNote(noteText) {
    const note = document.getElementById('daz-modal-currency-note');
    if (!note) {
      return;
    }
    note.textContent = noteText;
  }

  function readStorageString(key) {
    try {
      const value = window.localStorage.getItem(key);
      return typeof value === 'string' ? value.trim() : '';
    } catch (err) {
      return '';
    }
  }

  function parseQueryStringValue(paramName) {
    try {
      const params = new URLSearchParams(window.location.search || '');
      const value = params.get(paramName);
      return typeof value === 'string' ? value.trim() : '';
    } catch (err) {
      return '';
    }
  }

  function detectCurrentUsername() {
    const candidates = [];
    if (window.CLIENT && typeof window.CLIENT.name === 'string') {
      candidates.push(window.CLIENT.name);
    }
    if (window.USER && typeof window.USER.name === 'string') {
      candidates.push(window.USER.name);
    }
    if (wsState.welcome && typeof wsState.welcome.username === 'string') {
      candidates.push(wsState.welcome.username);
    }

    for (let i = 0; i < candidates.length; i += 1) {
      const candidate = (candidates[i] || '').trim();
      if (candidate) {
        return candidate;
      }
    }
    return '';
  }

  function detectCurrentChannel() {
    if (window.CHANNEL && typeof window.CHANNEL.name === 'string' && window.CHANNEL.name.trim()) {
      return window.CHANNEL.name.trim();
    }

    const path = window.location && typeof window.location.pathname === 'string'
      ? window.location.pathname
      : '';
    const roomMatch = path.match(/^\/r\/([^/]+)/i);
    if (roomMatch && roomMatch[1]) {
      return decodeURIComponent(roomMatch[1]).trim();
    }

    if (wsState.welcome && typeof wsState.welcome.default_channel === 'string') {
      return wsState.welcome.default_channel.trim();
    }

    return '';
  }

  function resolveWsBridgeToken() {
    const runtimeToken = typeof window.__DAZ_WSBRIDGE_TOKEN === 'string'
      ? window.__DAZ_WSBRIDGE_TOKEN.trim()
      : '';
    if (runtimeToken) {
      return runtimeToken;
    }

    const queryToken = parseQueryStringValue(WS_QUERY_TOKEN_KEY);
    if (queryToken) {
      return queryToken;
    }

    return readStorageString(WS_TOKEN_STORAGE_KEY);
  }

  function parseHostFromURL(rawURL) {
    if (!rawURL || typeof rawURL !== 'string') {
      return '';
    }
    try {
      const parsed = new URL(rawURL, window.location.href);
      return parsed.hostname ? parsed.hostname.trim() : '';
    } catch (err) {
      return '';
    }
  }

  function isBlockedDefaultHost(hostname) {
    const host = (hostname || '').trim().toLowerCase();
    if (!host) {
      return true;
    }

    const blockedSuffixes = [
      'cytu.be',
      'github.io',
      'github.com',
      'githubusercontent.com',
      'raw.githubusercontent.com',
    ];

    return blockedSuffixes.some((suffix) => host === suffix || host.endsWith(`.${suffix}`));
  }

  function resolveWsBridgeBaseURL() {
    const runtimeURL = typeof window.__DAZ_WSBRIDGE_URL === 'string'
      ? window.__DAZ_WSBRIDGE_URL.trim()
      : '';
    if (runtimeURL) {
      return runtimeURL;
    }

    const queryURL = parseQueryStringValue(WS_QUERY_URL_KEY);
    if (queryURL) {
      return queryURL;
    }

    const storedURL = readStorageString(WS_URL_STORAGE_KEY);
    if (storedURL) {
      return storedURL;
    }

    const runtimeHost = typeof window[WS_RUNTIME_HOST_KEY] === 'string'
      ? window[WS_RUNTIME_HOST_KEY].trim()
      : '';
    if (runtimeHost) {
      return `ws://${runtimeHost}:${WS_DEFAULT_PORT}${WS_DEFAULT_PATH}`;
    }

    const queryHost = parseQueryStringValue(WS_QUERY_HOST_KEY);
    if (queryHost) {
      return `ws://${queryHost}:${WS_DEFAULT_PORT}${WS_DEFAULT_PATH}`;
    }

    const loaderHost = LOADER_SOURCE && LOADER_SOURCE !== 'inline-or-unknown'
      ? parseHostFromURL(LOADER_SOURCE)
      : '';
    if (loaderHost && !isBlockedDefaultHost(loaderHost)) {
      return `ws://${loaderHost}:${WS_DEFAULT_PORT}${WS_DEFAULT_PATH}`;
    }

    const pageHost = window.location && typeof window.location.hostname === 'string'
      ? window.location.hostname.trim()
      : '';
    if (pageHost && !isBlockedDefaultHost(pageHost)) {
      return `ws://${pageHost}:${WS_DEFAULT_PORT}${WS_DEFAULT_PATH}`;
    }

    return '';
  }

  function resolveWsBridgeURL() {
    const token = resolveWsBridgeToken();
    if (!token) {
      return { url: '', token: '' };
    }

    const base = resolveWsBridgeBaseURL();
    if (!base) {
      return { url: '', token: '' };
    }

    try {
      const url = new URL(base, window.location.href);
      if (!url.pathname || url.pathname === '/') {
        url.pathname = WS_DEFAULT_PATH;
      }
      url.searchParams.set('token', token);
      return { url: url.toString(), token };
    } catch (err) {
      return { url: '', token: '' };
    }
  }

  function clearPendingWsRequests(reason) {
    wsState.pending.forEach((pending) => {
      try {
        window.clearTimeout(pending.timeoutID);
      } catch (err) {
        // no-op
      }
      pending.reject(new Error(reason || 'wsbridge request cancelled'));
    });
    wsState.pending.clear();
  }

  function sendWsBridgeRequest(type, payload) {
    return new Promise((resolve, reject) => {
      if (!wsState.socket || wsState.socket.readyState !== WebSocket.OPEN || !wsState.connected) {
        reject(new Error('wsbridge not connected'));
        return;
      }

      wsState.requestCounter += 1;
      const requestID = `ux-${Date.now()}-${wsState.requestCounter}`;
      const message = {
        id: requestID,
        type,
        payload: payload || {},
      };

      const timeoutID = window.setTimeout(() => {
        wsState.pending.delete(requestID);
        reject(new Error(`wsbridge request timeout (${type})`));
      }, WS_REQUEST_TIMEOUT_MS);

      wsState.pending.set(requestID, { resolve, reject, timeoutID, type });

      try {
        wsState.socket.send(JSON.stringify(message));
      } catch (err) {
        window.clearTimeout(timeoutID);
        wsState.pending.delete(requestID);
        reject(err instanceof Error ? err : new Error('wsbridge send failed'));
      }
    });
  }

  function applyLiveBalance(balanceValue, sourceLabel) {
    const numericBalance = Number(balanceValue);
    if (!Number.isFinite(numericBalance)) {
      return;
    }

    state.balance = Math.round(numericBalance);
    refreshBalance();
    saveState();

    const source = sourceLabel && typeof sourceLabel === 'string' ? sourceLabel : 'wsbridge';
    setCurrencyNote(`Live via ${source}`);
  }

  function requestLiveBalance(reason) {
    if (isDestroyed) {
      return;
    }
    if (wsState.balanceInFlight) {
      return;
    }
    if (!wsState.connected || !wsState.socket || wsState.socket.readyState !== WebSocket.OPEN) {
      return;
    }

    wsState.balanceInFlight = true;
    const username = detectCurrentUsername();
    const channel = detectCurrentChannel();
    const payload = {};
    if (username) {
      payload.username = username;
    }
    if (channel) {
      payload.channel = channel;
    }

    sendWsBridgeRequest('economy.get_balance', payload)
      .then((data) => {
        if (data && Object.prototype.hasOwnProperty.call(data, 'balance')) {
          applyLiveBalance(data.balance, 'wsbridge');
          return;
        }
        setCurrencyNote('Live balance unavailable');
      })
      .catch((err) => {
        const message = err && err.message ? err.message : 'unknown error';
        setCurrencyNote(`Live balance failed: ${message}`);
      })
      .finally(() => {
        wsState.balanceInFlight = false;
        if (reason === 'open') {
          window.setTimeout(() => requestLiveBalance('post-open-refresh'), 1200);
        }
      });
  }

  function scheduleWsReconnect() {
    if (isDestroyed) {
      return;
    }
    if (wsState.reconnectTimer) {
      return;
    }

    const backoff = Math.min(20000, 1000 * Math.max(1, wsState.reconnectAttempts));
    wsState.reconnectTimer = window.setTimeout(() => {
      wsState.reconnectTimer = null;
      connectWsBridge();
    }, backoff);
  }

  function onWsBridgeMessage(event) {
    if (isDestroyed) {
      return;
    }
    if (!event || typeof event.data !== 'string' || !event.data) {
      return;
    }

    let message;
    try {
      message = JSON.parse(event.data);
    } catch (err) {
      return;
    }

    if (!message || typeof message !== 'object') {
      return;
    }

    const messageType = typeof message.type === 'string' ? message.type : '';
    const requestID = typeof message.id === 'string' ? message.id : '';

    if (messageType === 'welcome') {
      wsState.welcome = message.data && typeof message.data === 'object' ? message.data : null;
      const welcomeUser = wsState.welcome && typeof wsState.welcome.username === 'string'
        ? wsState.welcome.username
        : '';
      setCurrencyNote(welcomeUser ? `Bridge connected as ${welcomeUser}` : 'Bridge connected');
      requestLiveBalance('welcome');
      return;
    }

    if (requestID && wsState.pending.has(requestID)) {
      const pending = wsState.pending.get(requestID);
      wsState.pending.delete(requestID);
      window.clearTimeout(pending.timeoutID);

      if (messageType === 'error') {
        const errorText = message.error && message.error.message ? message.error.message : 'wsbridge error';
        pending.reject(new Error(errorText));
        return;
      }

      pending.resolve(message.data);
      return;
    }
  }

  function connectWsBridge() {
    if (isDestroyed) {
      return;
    }
    const resolved = resolveWsBridgeURL();
    if (!resolved.token) {
      setCurrencyNote('Live balance offline (missing ws token)');
      return;
    }
    if (!resolved.url) {
      setCurrencyNote('Live balance offline (missing ws URL)');
      return;
    }

    if (wsState.socket && (wsState.socket.readyState === WebSocket.OPEN || wsState.socket.readyState === WebSocket.CONNECTING)) {
      return;
    }

    let socket;
    try {
      socket = new WebSocket(resolved.url);
    } catch (err) {
      setCurrencyNote('Live balance offline (socket init failed)');
      scheduleWsReconnect();
      return;
    }

    wsState.socket = socket;
    wsState.connected = false;

    socket.addEventListener('open', () => {
      wsState.connected = true;
      wsState.reconnectAttempts = 0;
      setCurrencyNote('Bridge connected');
      requestLiveBalance('open');
    });

    socket.addEventListener('message', onWsBridgeMessage);

    socket.addEventListener('close', () => {
      wsState.connected = false;
      clearPendingWsRequests('wsbridge disconnected');
      if (wsState.suppressReconnect) {
        wsState.suppressReconnect = false;
        return;
      }
      wsState.reconnectAttempts += 1;
      setCurrencyNote('Bridge reconnecting...');
      scheduleWsReconnect();
    });

    socket.addEventListener('error', () => {
      setCurrencyNote('Bridge connection error');
    });
  }

  function resetWsBridgeConnection() {
    clearPendingWsRequests('wsbridge reset');
    if (wsState.reconnectTimer) {
      window.clearTimeout(wsState.reconnectTimer);
      wsState.reconnectTimer = null;
    }
    wsState.connected = false;
    wsState.reconnectAttempts = 0;
    if (wsState.socket) {
      try {
        wsState.suppressReconnect = true;
        wsState.socket.close(1000, 'reset');
      } catch (err) {
        // no-op
      }
      wsState.socket = null;
    }
  }

  function ensureBalanceRefreshLoop() {
    if (isDestroyed) {
      return;
    }
    if (wsState.periodicRefreshTimer) {
      return;
    }

    wsState.periodicRefreshTimer = window.setInterval(() => {
      if (document.visibilityState === 'hidden') {
        return;
      }
      requestLiveBalance('interval');
    }, WS_BALANCE_REFRESH_MS);
  }

  function handleVisibilityRefresh() {
    if (isDestroyed) {
      return;
    }
    if (document.visibilityState === 'visible') {
      requestLiveBalance('visibility');
    }
  }

  window.__dazGameModalSetWsToken = function setWsToken(token) {
    const value = typeof token === 'string' ? token.trim() : '';
    try {
      if (value) {
        window.localStorage.setItem(WS_TOKEN_STORAGE_KEY, value);
      } else {
        window.localStorage.removeItem(WS_TOKEN_STORAGE_KEY);
      }
    } catch (err) {
      // no-op
    }
    resetWsBridgeConnection();
    connectWsBridge();
    requestLiveBalance('set-token');
  };

  window.__dazGameModalSetWsUrl = function setWsUrl(url) {
    const value = typeof url === 'string' ? url.trim() : '';
    try {
      if (value) {
        window.localStorage.setItem(WS_URL_STORAGE_KEY, value);
      } else {
        window.localStorage.removeItem(WS_URL_STORAGE_KEY);
      }
    } catch (err) {
      // no-op
    }
    resetWsBridgeConnection();
    connectWsBridge();
    requestLiveBalance('set-url');
  };

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
    boundRoot = root;
    root.addEventListener('click', onClick, true);
    root.addEventListener('mousedown', onMouseDown);
    root.addEventListener('touchstart', onMouseDown);
    window.addEventListener('resize', applyGeometry);
    window.addEventListener('orientationchange', applyGeometry);
    document.addEventListener('visibilitychange', handleVisibilityRefresh);
    window.addEventListener('pagehide', flushStateNow);
    window.addEventListener('beforeunload', flushStateNow);
  }

  function unbindEvents() {
    if (boundRoot) {
      boundRoot.removeEventListener('click', onClick, true);
      boundRoot.removeEventListener('mousedown', onMouseDown);
      boundRoot.removeEventListener('touchstart', onMouseDown);
      boundRoot = null;
    }
    window.removeEventListener('resize', applyGeometry);
    window.removeEventListener('orientationchange', applyGeometry);
    document.removeEventListener('visibilitychange', handleVisibilityRefresh);
    window.removeEventListener('pagehide', flushStateNow);
    window.removeEventListener('beforeunload', flushStateNow);
  }

  function cleanupModal(options) {
    const opts = options && typeof options === 'object' ? options : {};
    isDestroyed = true;
    onInteractionStop();
    flushStateNow();
    unbindEvents();

    if (wsState.periodicRefreshTimer) {
      window.clearInterval(wsState.periodicRefreshTimer);
      wsState.periodicRefreshTimer = null;
    }

    resetWsBridgeConnection();

    if (opts.removeRoot) {
      const root = document.getElementById('daz-game-modal-root');
      if (root) {
        root.remove();
      }
    }

    if (window.__dazGameModalBuild === LOADER_BUILD_ID) {
      window.__dazGameModalActive = false;
    }
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
    connectWsBridge();
    ensureBalanceRefreshLoop();
    requestLiveBalance('mount');
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

  window.__dazGameModalCleanup = cleanupModal;

  bootstrap();
})();
