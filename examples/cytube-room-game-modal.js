(function () {
  'use strict';

  const existingRoot = document.getElementById('daz-game-modal-root');
  if (window.__dazGameModalActive && existingRoot) {
    return;
  }
  window.__dazGameModalActive = true;
  const statusId = 'daz-game-modal-inline-status';

  function showInlineStatus(message, asError) {
    const status = document.getElementById(statusId) || document.createElement('div');
    status.id = statusId;
    status.textContent = message;
    status.style.cssText =
      'position:fixed;left:12px;bottom:12px;z-index:2147483647;background:' +
      (asError ? '#4a120f' : '#2d1810') +
      ';color:#d4af37;border:1px solid rgba(212,175,55,.45);padding:6px 10px;font:12px/1.3 Cinzel,Georgia,serif;max-width:40vw;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;box-shadow:0 0 12px rgba(0,0,0,.35);';
    (document.body || document.documentElement).appendChild(status);
  }

  function hideInlineStatus() {
    const status = document.getElementById(statusId);
    if (status) {
      status.remove();
    }
  }

  showInlineStatus('daz game modal: initializing...');

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
  const STORAGE_RESTORE_LEFT_KEY = 'daz-cytube-game-modal-restore-left-v1';
  const STORAGE_RESTORE_TOP_KEY = 'daz-cytube-game-modal-restore-top-v1';
  const STORAGE_RESTORE_WIDTH_KEY = 'daz-cytube-game-modal-restore-width-v1';
  const STORAGE_RESTORE_HEIGHT_KEY = 'daz-cytube-game-modal-restore-height-v1';
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
    left: null,
    top: null,
    width: null,
    height: null,
    restoreLeft: null,
    restoreTop: null,
    restoreWidth: null,
    restoreHeight: null,
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

      state.left = parseStoredNumber(window.localStorage.getItem(STORAGE_LEFT_KEY), null);
      state.top = parseStoredNumber(window.localStorage.getItem(STORAGE_TOP_KEY), null);
      state.width = parseStoredNumber(window.localStorage.getItem(STORAGE_WIDTH_KEY), null);
      state.height = parseStoredNumber(window.localStorage.getItem(STORAGE_HEIGHT_KEY), null);
      state.restoreLeft = parseStoredNumber(window.localStorage.getItem(STORAGE_RESTORE_LEFT_KEY), null);
      state.restoreTop = parseStoredNumber(window.localStorage.getItem(STORAGE_RESTORE_TOP_KEY), null);
      state.restoreWidth = parseStoredNumber(window.localStorage.getItem(STORAGE_RESTORE_WIDTH_KEY), null);
      state.restoreHeight = parseStoredNumber(window.localStorage.getItem(STORAGE_RESTORE_HEIGHT_KEY), null);
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
    } catch (err) {
      // ignore storage failures.
    }
  }

  function getUiModuleBase() {
    const current = document.currentScript;
    if (!current || !current.src) {
      return null;
    }
    const src = current.src;
    return src.substring(0, src.lastIndexOf('/') + 1);
  }

  const UI_BASE = getUiModuleBase();
  const UI_MODULE_FILES = [
    'cytube-room-game-modal-modules/legacy/daz-game-modal-view.js',
    'cytube-room-game-modal-modules/legacy/daz-game-modal-needs.js',
  ].map((path) => `${UI_BASE || ''}${path}`);
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
    showInlineStatus('daz game modal: loading ui modules...');
    for (const url of UI_MODULE_FILES) {
      await loadUiModule(url);
    }
    if (!window.__dazGameModalView || !window.__dazGameModalNeeds) {
      throw new Error('daz game modal: ui modules did not initialize');
    }
    uiViewLoaded = true;
    hideInlineStatus();
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

  function getPlaceholderMessage(action) {
    if (!uiViewLoaded || !window.__dazGameModalView || typeof window.__dazGameModalView.placeholderMessageFor !== 'function') {
      return null;
    }
    return window.__dazGameModalView.placeholderMessageFor(action);
  }

  function formatMessage(message) {
    const now = new Date();
    const stamp = now.toLocaleTimeString([], { hour12: false });
    return `<strong>[${stamp}]</strong> ${message}`;
  }

  function appendLog(message) {
    const log = document.getElementById('daz-game-modal-log');
    if (!log) {
      return;
    }
    const line = document.createElement('div');
    line.className = 'daz-log-entry';
    line.innerHTML = formatMessage(message);
    log.appendChild(line);
    log.scrollTop = log.scrollHeight;
    while (log.children.length > 24) {
      log.removeChild(log.firstElementChild);
    }
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

    Object.entries(state.needs).forEach(([key, value]) => {
      const label = document.getElementById(`daz-modal-need-${key}`);
      const bar = document.getElementById(`daz-modal-need-${key}-bar`);
      if (label) {
        label.textContent = String(value);
      }
      if (bar) {
        bar.style.width = `${clamp(value, MIN_NEED, MAX_NEED)}%`;
      }
    });
  }

  function onAction(action) {
    const msg = getPlaceholderMessage(action);
    if (!msg) {
      return;
    }
    if (action === 'money-add') {
      state.balance += 50;
      refreshBalance();
    } else if (action === 'money-spend') {
      state.balance = Math.max(0, state.balance - 25);
      refreshBalance();
    } else if (action === 'need-bathroom') {
      state.needs.bladder = clamp(state.needs.bladder + 15, MIN_NEED, MAX_NEED);
      refreshNeeds();
    } else if (action === 'need-eat') {
      state.needs.food = clamp(state.needs.food + 10, MIN_NEED, MAX_NEED);
      refreshNeeds();
    } else if (action === 'need-drink') {
      state.needs.alcohol = clamp(state.needs.alcohol + 12, MIN_NEED, MAX_NEED);
      refreshNeeds();
    } else if (action === 'need-weed') {
      state.needs.weed = clamp(state.needs.weed + 10, MIN_NEED, MAX_NEED);
      refreshNeeds();
    } else if (action === 'need-lust') {
      state.needs.lust = clamp(state.needs.lust + 8, MIN_NEED, MAX_NEED);
      refreshNeeds();
    } else if (action === 'needs-recover') {
      state.needs.bladder = clamp(state.needs.bladder + 20, MIN_NEED, MAX_NEED);
      state.needs.food = clamp(state.needs.food + 20, MIN_NEED, MAX_NEED);
      state.needs.alcohol = clamp(state.needs.alcohol + 20, MIN_NEED, MAX_NEED);
      state.needs.weed = clamp(state.needs.weed + 20, MIN_NEED, MAX_NEED);
      state.needs.lust = clamp(state.needs.lust + 20, MIN_NEED, MAX_NEED);
      refreshNeeds();
    }
    appendLog(msg);
    saveState();
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

  function onMouseDown(event) {
    const isMouse = event.type === 'mousedown';
    if (isMouse && event.button !== 0) {
      return;
    }
    const root = document.getElementById('daz-game-modal-root');
    if (!root) {
      return;
    }
    const actionTarget = event.target.closest('[data-action]');
    if (actionTarget) {
      return;
    }
    if (event.target.closest('#daz-game-modal-resize-handle')) {
      beginInteraction(event, 'resize');
      return;
    }
    if (event.target.closest('#daz-game-modal-header')) {
      beginInteraction(event, 'drag');
    }
  }

  function onClick(event) {
    const actionTarget = event.target.closest('[data-action]');
    if (actionTarget) {
      const action = actionTarget.getAttribute('data-action');
      if (action === 'toggle-min') {
        event.preventDefault();
        event.stopPropagation();
        setMode(state.mode === 'minimized' ? 'open' : 'minimized');
        return;
      }
      onAction(action);
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

  function enforceFallbackVisuals(root) {
    if (!root || root.dataset.fallbackApplied === '1') {
      return;
    }
    root.dataset.fallbackApplied = '1';
    root.style.position = 'fixed';
    root.style.setProperty('display', 'block', 'important');
    root.style.setProperty('visibility', 'visible', 'important');
    root.style.setProperty('z-index', '2147483647', 'important');
    root.style.setProperty('background', '#140f0a', 'important');
    root.style.setProperty('border', '1px solid rgba(212,175,55,.35)', 'important');
    root.style.setProperty('border-radius', '8px', 'important');
    root.style.setProperty('color', '#d4af37', 'important');
    root.style.setProperty('pointer-events', 'auto', 'important');
    root.style.setProperty('user-select', 'none', 'important');
    root.style.setProperty('font-family', 'Cinzel, Georgia, serif', 'important');
    root.style.setProperty('letter-spacing', '0.5px', 'important');
  }

  function mount() {
    if (document.getElementById('daz-game-modal-root')) {
      return;
    }

    const root = document.createElement('div');
    root.id = 'daz-game-modal-root';
    root.innerHTML = createMarkup();

    maybeInjectStyles();
    const container = document.body || document.documentElement;
    container.appendChild(root);
    enforceFallbackVisuals(root);
    applyGeometry();
    console.info('[daz-game-modal] mounted', { rootId: root.id });
    hideInlineStatus();
    bindEvents();

    refreshBalance();
    refreshNeeds();
    updateModeUI();
    appendLog('Compact placeholder UI loaded. Replace actions with real command wiring when ready.');
    appendLog('Minimize is preserved so users can reclaim viewport whenever needed.');
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
      console.info('[daz-game-modal] bundle loaded; root exists now?', !!document.getElementById('daz-game-modal-root'));
    } catch (err) {
      console.error('[daz-game-modal] Failed to mount modal:', err);
      window.__dazGameModalLoadError = true;
      showInlineStatus(`daz game modal: failed to mount (${err && err.message ? err.message : 'unknown error'})`, true);
    }
  }

  bootstrap();
})();
