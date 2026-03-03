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
  const STORAGE_TAB_KEY = 'daz-cytube-game-modal-tab-v1';
  const DEFAULT_BALANCE = 1250;

  const state = {
    mode: 'open',
    balance: DEFAULT_BALANCE,
    tab: 'home',
  };

  function loadState() {
    try {
      const storedMode = window.localStorage.getItem(STORAGE_MODE_KEY);
      if (storedMode === 'open' || storedMode === 'minimized') {
        state.mode = storedMode;
      }

      const storedTab = window.localStorage.getItem(STORAGE_TAB_KEY);
      if (storedTab === 'home' || storedTab === 'fishing' || storedTab === 'eightball' || storedTab === 'piss') {
        state.tab = storedTab;
      }

      const storedBalance = parseInt(window.localStorage.getItem(STORAGE_BALANCE_KEY), 10);
      if (!Number.isNaN(storedBalance)) {
        state.balance = storedBalance;
      }
    } catch (err) {
      // localStorage unavailable in restricted contexts.
    }
  }

  function saveState() {
    try {
      window.localStorage.setItem(STORAGE_MODE_KEY, state.mode);
      window.localStorage.setItem(STORAGE_BALANCE_KEY, String(state.balance));
      window.localStorage.setItem(STORAGE_TAB_KEY, state.tab);
    } catch (err) {
      // ignore storage failures.
    }
  }

  function cssText() {
    return `
      #daz-game-modal-root {
        position: fixed;
        left: 12px;
        bottom: 12px;
        display: block !important;
        visibility: visible !important;
        width: clamp(320px, 34vw, 700px);
        height: clamp(250px, 36vh, 420px);
        z-index: 2147483647;
        font-family: Cinzel, Georgia, serif;
        pointer-events: auto !important;
        color: #d4af37;
        letter-spacing: 0.5px;
        user-select: none;
        text-transform: uppercase;
      }

      #daz-game-modal-root * {
        box-sizing: border-box;
      }

      #daz-game-modal-root::before {
        content: "☘ PADDY'S PUB MENU ☘";
        position: absolute;
        left: 0;
        right: 0;
        top: -22px;
        height: 22px;
        font-family: "Irish Grover", cursive;
        font-size: 11px;
        color: #d4af37;
        background: linear-gradient(90deg, #169b62 0, #169b62 33.33%, #fff 33.33%, #fff 66.66%, #ff883e 66.66%, #ff883e 100%);
        display: flex;
        align-items: center;
        justify-content: center;
        border: 1px solid rgba(212, 175, 55, 0.3);
        box-shadow: 0 2px 12px rgba(0, 0, 0, 0.5);
        z-index: 1;
      }

      #daz-game-modal {
        border-radius: 8px;
        border: 1px solid rgba(212, 175, 55, 0.35);
        background: linear-gradient(135deg, #140f0a 0, #0a0604 100%);
        box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.05), inset 0 -20px 30px rgba(26, 15, 8, 0.45), 0 12px 30px rgba(0, 0, 0, 0.75);
        overflow: hidden;
        position: relative;
        width: 100%;
        height: 100%;
      }

      #daz-game-modal::before {
        content: "";
        position: absolute;
        inset: 0;
        pointer-events: none;
        opacity: 0.14;
        background: repeating-linear-gradient(90deg, rgba(255, 255, 255, 0.05) 0, rgba(255, 255, 255, 0.05) 2px, transparent 2px, transparent 4px);
      }

      #daz-game-modal-header {
        height: 36px;
        border-bottom: 1px solid rgba(212, 175, 55, 0.3);
        background: linear-gradient(180deg, rgba(22, 155, 98, 0.6) 0, rgba(22, 155, 98, 0.6) 2px, rgba(255, 255, 255, 0.4) 2px, rgba(255, 255, 255, 0.4) 4px, rgba(255, 136, 62, 0.6) 4px, rgba(255, 136, 62, 0.6) 6px, transparent 6px, transparent 100%), linear-gradient(180deg, #1a0f08 0, #0d0704 100%);
        display: flex;
        align-items: center;
        justify-content: space-between;
        padding: 0 8px;
        position: relative;
        z-index: 2;
      }

      #daz-game-modal-title {
        font-family: "Irish Grover", cursive;
        font-size: 16px;
        color: #d4af37;
        text-shadow: 1px 1px 2px #000;
        line-height: 1.1;
        white-space: nowrap;
      }

      .daz-game-modal-btn {
        border: 1px solid rgba(212, 175, 55, 0.3);
        border-radius: 3px;
        min-width: 30px;
        height: 22px;
        color: #d4af37;
        background: linear-gradient(135deg, #2d1810 0, #1d1410 100%);
        cursor: pointer;
        padding: 0 8px;
        transition: 0.3s;
        text-transform: uppercase;
        font-family: Cinzel, serif;
        font-size: 12px;
        box-shadow: 0 2px 5px rgba(0, 0, 0, 0.5), inset 0 1px 0 rgba(212, 175, 55, 0.1);
      }

      .daz-game-modal-btn:hover {
        background: linear-gradient(135deg, #4d3830 0, #3d2820 50%, #2d1810 100%);
        color: gold;
        box-shadow: 0 0 20px rgba(212, 175, 55, 0.5), inset 0 0 10px rgba(212, 175, 55, 0.2);
        transform: translateY(-1px);
      }

      #daz-game-modal-body {
        position: relative;
        z-index: 2;
        height: calc(100% - 36px);
        display: flex;
        flex-direction: column;
        gap: 5px;
        min-height: 0;
      }

      #daz-game-modal-summary {
        padding: 8px 9px 3px;
        display: grid;
        grid-template-columns: 1fr 1fr;
        gap: 6px;
        font-size: 11px;
        color: #a08050;
      }

      #daz-game-modal-summary strong {
        display: block;
        margin-top: 2px;
        font-size: 14px;
        color: #d4af37;
        letter-spacing: 0.7px;
      }

      #daz-game-modal-tabs {
        display: grid;
        grid-template-columns: repeat(4, 1fr);
        gap: 3px;
        padding: 0 8px;
      }

      .daz-game-tab {
        height: 28px;
        border: 1px solid rgba(212, 175, 55, 0.3);
        color: #c9a961;
        background: linear-gradient(135deg, #2d1810 0, #1d1410 100%);
        border-radius: 4px 4px 0 0;
        font-family: Cinzel, serif;
        cursor: pointer;
        transition: 0.3s;
        font-size: 11px;
        letter-spacing: 0.4px;
      }

      .daz-game-tab:hover {
        background: linear-gradient(135deg, #4d3830 0, #3d2820 50%, #2d1810 100%);
        color: #fff0a0;
      }

      .daz-game-tab[aria-selected="true"] {
        background: linear-gradient(180deg, rgba(22, 155, 98, 0.48) 0, rgba(22, 155, 98, 0.35) 100%);
        color: #ffd76a;
        border-color: rgba(255, 215, 106, 0.75);
      }

      #daz-game-modal-panels {
        flex: 1;
        min-height: 0;
        padding: 0 8px;
        display: flex;
        flex-direction: column;
      }

      .daz-game-tab-panel {
        display: none;
        flex-direction: column;
        gap: 6px;
        min-height: 0;
      }

      .daz-game-tab-panel[aria-hidden="false"] {
        display: flex;
      }

      .daz-game-section {
        border: 1px solid rgba(212, 175, 55, 0.2);
        border-radius: 6px;
        background: radial-gradient(circle at 20% 30%, rgba(139, 105, 20, 0.12) 0, transparent 30%), radial-gradient(circle at 80% 70%, rgba(139, 105, 20, 0.08) 0, transparent 25%), linear-gradient(180deg, rgba(26, 15, 8, 0.9) 0, rgba(15, 12, 10, 0.95) 100%);
        padding: 6px;
      }

      .daz-game-section-title {
        margin: 0 0 5px;
        font-size: 11px;
        color: #d4af37;
      }

      .daz-game-grid {
        display: grid;
        grid-template-columns: repeat(2, minmax(0, 1fr));
        gap: 6px;
      }

      .daz-game-action-btn {
        border: 1px solid rgba(212, 175, 55, 0.3);
        border-radius: 3px;
        min-height: 28px;
        color: #d4af37;
        cursor: pointer;
        text-transform: uppercase;
        background: linear-gradient(135deg, #2d1810 0, #1d1410 100%);
        font-family: Cinzel, serif;
        box-shadow: 0 2px 5px rgba(0, 0, 0, 0.5), inset 0 1px 0 rgba(212, 175, 55, 0.1);
        transition: 0.3s;
      }

      .daz-game-action-btn:hover {
        background: linear-gradient(135deg, #4d3830 0, #3d2820 50%, #2d1810 100%);
        color: gold;
        box-shadow: 0 0 20px rgba(212, 175, 55, 0.5), inset 0 0 10px rgba(212, 175, 55, 0.2);
      }

      .daz-game-action-btn.secondary {
        background: linear-gradient(135deg, #173820 0, #0f2717 100%);
        border-color: rgba(22, 155, 98, 0.6);
      }

      .daz-game-action-btn.secondary:hover {
        background: linear-gradient(135deg, #1a5c40 0, #164a34 100%);
      }

      #daz-game-modal-log-wrap {
        padding: 4px 8px 8px;
      }

      #daz-game-modal-log-wrap .daz-game-section-title {
        margin: 0 0 4px;
      }

      #daz-game-modal-log {
        height: clamp(62px, 22%, 94px);
        overflow: auto;
        border: 1px solid rgba(212, 175, 55, 0.2);
        border-radius: 6px;
        background: radial-gradient(circle at 20% 30%, rgba(139, 105, 20, 0.1) 0, transparent 15%), radial-gradient(circle at 80% 70%, rgba(139, 105, 20, 0.08) 0, transparent 15%), radial-gradient(circle at 60% 40%, rgba(139, 105, 20, 0.06) 0, transparent 10%), radial-gradient(ellipse at 50% 0, rgba(212, 175, 55, 0.03) 0, transparent 50%), rgba(15, 12, 10, 0.95);
        padding: 6px;
        font-size: 12px;
        line-height: 1.35;
        font-family: Georgia, serif;
        color: #c9a961;
      }

      .daz-log-entry {
        margin-bottom: 4px;
        color: #c9a961;
      }

      .daz-log-entry strong {
        color: #d4af37;
      }

      #daz-game-modal-root.daz-state-minimized {
        width: 220px;
        height: 38px;
      }

      #daz-game-modal-root.daz-state-minimized #daz-game-modal-body {
        display: none;
      }

      #daz-game-modal-root.daz-state-minimized #daz-game-modal {
        width: 100%;
        height: 38px;
      }

      #daz-game-modal-root.daz-state-minimized::before {
        display: none;
      }

      @media (min-width: 1600px) {
        #daz-game-modal-root {
          width: clamp(420px, 28vw, 860px);
          height: clamp(300px, 34vh, 460px);
        }
      }

      @media (max-width: 1200px) {
        #daz-game-modal-root {
          width: min(96vw, 620px);
          height: clamp(250px, 45vh, 400px);
        }
      }

      @media (max-width: 680px) {
        #daz-game-modal-root {
          left: 8px;
          bottom: 8px;
          width: calc(100vw - 10px);
          height: clamp(260px, 56vh, 430px);
        }

        #daz-game-modal-tabs {
          grid-template-columns: repeat(2, 1fr);
          grid-auto-rows: 30px;
        }

        #daz-game-modal-title {
          font-size: 15px;
        }
      }
    `;
  }

  function formatMessage(message) {
    const now = new Date();
    const stamp = now.toLocaleTimeString([], { hour12: false });
    return `<strong>[${stamp}]</strong> ${message}`;
  }

  function createMarkup() {
    return `
      <div id="daz-game-modal">
        <div id="daz-game-modal-header">
          <div id="daz-game-modal-title">Paddy's Pub Game Console</div>
          <div id="daz-game-modal-actions">
            <button type="button" class="daz-game-modal-btn" data-action="toggle-min" title="Minimise">_</button>
          </div>
        </div>
        <div id="daz-game-modal-body">
          <div id="daz-game-modal-summary">
            <div>Mode <strong id="daz-modal-state">open</strong></div>
            <div>Balance <strong id="daz-modal-balance">0</strong></div>
          </div>

          <div id="daz-game-modal-tabs" role="tablist" aria-label="Game sections">
            <button type="button" class="daz-game-tab" data-tab="home" aria-selected="false">Home</button>
            <button type="button" class="daz-game-tab" data-tab="fishing" aria-selected="false">Fishing</button>
            <button type="button" class="daz-game-tab" data-tab="eightball" aria-selected="false">8-Ball</button>
            <button type="button" class="daz-game-tab" data-tab="piss" aria-selected="false">Piss</button>
          </div>

          <div id="daz-game-modal-panels">
            <section class="daz-game-tab-panel" data-tab-panel="home" aria-hidden="true">
              <div class="daz-game-section">
                <h4 class="daz-game-section-title">Quick actions</h4>
                <div class="daz-game-grid">
                  <button type="button" class="daz-game-action-btn" data-action="fish">Fish</button>
                  <button type="button" class="daz-game-action-btn" data-action="8ball">8-Ball</button>
                  <button type="button" class="daz-game-action-btn secondary" data-action="piss">Piss</button>
                  <button type="button" class="daz-game-action-btn secondary" data-action="status">Status</button>
                </div>
              </div>
              <div class="daz-game-section">
                <h4 class="daz-game-section-title">Sub-chat placeholder</h4>
                <div style="font-size: 11px; color: #a08050; letter-spacing: .2px;">Dedicated area for command responses and local previews.</div>
              </div>
            </section>

            <section class="daz-game-tab-panel" data-tab-panel="fishing" aria-hidden="true">
              <div class="daz-game-section">
                <h4 class="daz-game-section-title">Fishing tools</h4>
                <div class="daz-game-grid">
                  <button type="button" class="daz-game-action-btn" data-action="bait-small">Small bait</button>
                  <button type="button" class="daz-game-action-btn" data-action="bait-large">Large bait</button>
                  <button type="button" class="daz-game-action-btn" data-action="bait-gold">Golden bait</button>
                  <button type="button" class="daz-game-action-btn" data-action="fish-reset">Reset line</button>
                </div>
              </div>
              <div class="daz-game-section">
                <h4 class="daz-game-section-title">Result placeholders</h4>
                <div class="daz-game-grid">
                  <button type="button" class="daz-game-action-btn secondary" data-action="fish-cast">Cast</button>
                  <button type="button" class="daz-game-action-btn secondary" data-action="fish-reel">Reel</button>
                </div>
              </div>
            </section>

            <section class="daz-game-tab-panel" data-tab-panel="eightball" aria-hidden="true">
              <div class="daz-game-section">
                <h4 class="daz-game-section-title">8-Ball actions</h4>
                <div class="daz-game-grid">
                  <button type="button" class="daz-game-action-btn" data-action="8ball-ask">Ask</button>
                  <button type="button" class="daz-game-action-btn" data-action="8ball-roll">Shake</button>
                  <button type="button" class="daz-game-action-btn secondary" data-action="8ball-magic">Random</button>
                  <button type="button" class="daz-game-action-btn secondary" data-action="8ball-reset">Reset</button>
                </div>
              </div>
            </section>

            <section class="daz-game-tab-panel" data-tab-panel="piss" aria-hidden="true">
              <div class="daz-game-section">
                <h4 class="daz-game-section-title">Piss contest controls</h4>
                <div class="daz-game-grid">
                  <button type="button" class="daz-game-action-btn" data-action="piss-start">Start</button>
                  <button type="button" class="daz-game-action-btn" data-action="piss-stats">Stats</button>
                  <button type="button" class="daz-game-action-btn secondary" data-action="piss-micro">Mini-game</button>
                  <button type="button" class="daz-game-action-btn secondary" data-action="piss-quit">Quit</button>
                </div>
              </div>
            </section>
          </div>

          <div id="daz-game-modal-log-wrap">
            <div class="daz-game-section-title">Sub-chat</div>
            <div id="daz-game-modal-log" aria-live="polite"></div>
          </div>
        </div>
      </div>
    `;
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
    const modeTag = document.getElementById('daz-modal-state');
    if (root && modeTag) {
      root.classList.toggle('daz-state-minimized', state.mode === 'minimized');
      modeTag.textContent = state.mode;
    }
    saveState();
  }

  function setMode(nextMode) {
    if (nextMode !== 'open' && nextMode !== 'minimized') {
      return;
    }
    state.mode = nextMode;
    updateModeUI();
  }

  function setActiveTab(nextTab) {
    state.tab = nextTab;
    const root = document.getElementById('daz-game-modal-root');
    if (!root) {
      return;
    }

    const tabs = root.querySelectorAll('.daz-game-tab');
    const panels = root.querySelectorAll('.daz-game-tab-panel');
    tabs.forEach((tab) => {
      const active = tab.dataset.tab === nextTab;
      tab.setAttribute('aria-selected', active ? 'true' : 'false');
    });
    panels.forEach((panel) => {
      const active = panel.dataset.tabPanel === nextTab;
      panel.setAttribute('aria-hidden', active ? 'false' : 'true');
    });

    saveState();
  }

  function refreshBalance() {
    const balance = document.getElementById('daz-modal-balance');
    if (balance) {
      balance.textContent = String(state.balance);
    }
  }

  const placeholderText = {
    fish: 'Placeholder: launch fish action payload',
    '8ball': 'Placeholder: launch 8-ball action payload',
    piss: 'Placeholder: launch piss command payload',
    status: 'Placeholder: open status pane',
    'bait-small': 'Placeholder: small bait',
    'bait-large': 'Placeholder: large bait',
    'bait-gold': 'Placeholder: golden bait',
    'fish-reset': 'Placeholder: reset fish state',
    'fish-cast': 'Placeholder: cast the line',
    'fish-reel': 'Placeholder: reel in the catch',
    '8ball-ask': 'Placeholder: ask custom question',
    '8ball-roll': 'Placeholder: roll answer',
    '8ball-magic': 'Placeholder: spin magic',
    '8ball-reset': 'Placeholder: clear 8-ball state',
    'piss-start': 'Placeholder: start piss contest',
    'piss-stats': 'Placeholder: show piss stats',
    'piss-micro': 'Placeholder: run piss mini-game',
    'piss-quit': 'Placeholder: quit contest',
  };

  function onAction(action) {
    const msg = placeholderText[action];
    if (!msg) {
      return;
    }
    appendLog(msg);
    state.balance += 10;
    refreshBalance();
    saveState();
  }

  function bindEvents() {
    const root = document.getElementById('daz-game-modal-root');
    if (!root) return;

    root.addEventListener('click', function (event) {
      const actionTarget = event.target.closest('[data-action]');
      if (actionTarget) {
        const action = actionTarget.getAttribute('data-action');
        if (action === 'toggle-min') {
          setMode(state.mode === 'minimized' ? 'open' : 'minimized');
          return;
        }
        onAction(action);
        return;
      }

      const tabTarget = event.target.closest('[data-tab]');
      if (tabTarget) {
        const nextTab = tabTarget.getAttribute('data-tab');
        if (nextTab) {
          setActiveTab(nextTab);
        }
      }
    }, true);
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
    forceBottomLeftPlacement(root);
    root.style.setProperty('width', 'clamp(320px, 34vw, 700px)', 'important');
    root.style.setProperty('height', 'clamp(250px, 36vh, 420px)', 'important');
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

  function forceBottomLeftPlacement(root) {
    if (!root) {
      return;
    }
    root.style.setProperty('position', 'fixed', 'important');
    root.style.setProperty('left', '12px', 'important');
    root.style.setProperty('right', 'auto', 'important');
    root.style.setProperty('top', 'auto', 'important');
    root.style.setProperty('bottom', '12px', 'important');
    root.style.setProperty('transform', 'none', 'important');
    root.style.setProperty('opacity', '1', 'important');
    root.style.setProperty('margin', '0', 'important');
    root.style.setProperty('inset', 'auto auto 12px 12px', 'important');
  }

  function keepBottomLeftAnchored() {
    const root = document.getElementById('daz-game-modal-root');
    if (!root) {
      return;
    }
    forceBottomLeftPlacement(root);
    const rect = root.getBoundingClientRect();
    const hiddenOrShifted = rect.width < 40 || rect.height < 40 || rect.left < -1 || rect.top > window.innerHeight || rect.right < 0 || rect.bottom > window.innerHeight + 4;
    if (hiddenOrShifted) {
      root.style.setProperty('outline', '2px solid rgba(255,165,0,0.85)', 'important');
    } else {
      root.style.removeProperty('outline');
    }
  }

  function startPlacementWatch() {
    let attempts = 0;
    const maxAttempts = 24;
    const interval = window.setInterval(() => {
      attempts += 1;
      keepBottomLeftAnchored();
      if (attempts >= maxAttempts) {
        window.clearInterval(interval);
      }
    }, 500);
    keepBottomLeftAnchored();
    window.addEventListener('resize', keepBottomLeftAnchored);
    window.addEventListener('orientationchange', keepBottomLeftAnchored);
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
    startPlacementWatch();
    console.info('[daz-game-modal] mounted', { rootId: root.id });
    hideInlineStatus();
    bindEvents();

    refreshBalance();
    setActiveTab(state.tab);
    updateModeUI();
    appendLog('Skeleton loaded. Replace action handlers with real game command wiring when ready.');
    appendLog('Minimize is preserved so users can reclaim viewport whenever needed.');
    saveState();
  }

  try {
    loadState();
    if (document.body) {
      mount();
    } else {
      window.addEventListener('DOMContentLoaded', mount, { once: true });
    }
    console.info('[daz-game-modal] bundle loaded; root exists now?', !!document.getElementById('daz-game-modal-root'));
  } catch (err) {
    console.error('[daz-game-modal] Failed to mount modal:', err);
    showInlineStatus('daz game modal: failed to mount', true);
  }
})();
