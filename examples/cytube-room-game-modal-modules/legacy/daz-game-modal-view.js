(function () {
  'use strict';

  window.__dazGameModalView = {
    cssText: () => `
      #daz-game-modal-root {
        position: fixed;
        left: 12px;
        top: auto;
        bottom: 12px;
        display: block !important;
        visibility: visible !important;
        width: clamp(340px, 34vw, 760px);
        height: clamp(280px, 38vh, 460px);
        z-index: 2147483647;
        font-family: Cinzel, Georgia, serif;
        pointer-events: auto !important;
        touch-action: none;
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

      #daz-game-modal-header {
        height: 36px;
        border-bottom: 1px solid rgba(212, 175, 55, 0.3);
        background: linear-gradient(180deg, rgba(22, 155, 98, 0.6) 0, rgba(22, 155, 98, 0.6) 2px, rgba(255, 255, 255, 0.4) 2px, rgba(255, 255, 255, 0.4) 4px, rgba(255, 136, 62, 0.6) 4px, rgba(255, 136, 62, 0.6) 6px, transparent 6px, transparent 100%), linear-gradient(180deg, #1a0f08 0, #0d0704 100%);
        display: flex;
        align-items: center;
        justify-content: flex-end;
        padding: 0 8px;
        position: relative;
        z-index: 2;
        cursor: move;
        touch-action: none;
        user-select: none;
      }

      #daz-game-modal-header-actions {
        display: flex;
        gap: 4px;
        justify-self: end;
        grid-column: 3;
      }

      .daz-game-modal-btn {
        border: 1px solid rgba(212, 175, 55, 0.3);
        border-radius: 3px;
        min-width: 30px;
        height: 22px;
        line-height: 1;
        color: #d4af37;
        display: inline-flex;
        align-items: center;
        justify-content: center;
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
        padding: 6px;
      }

      #daz-game-modal-summary {
        display: flex;
        flex-direction: column;
        gap: 6px;
        font-size: 11px;
        color: #a08050;
        align-items: stretch;
      }

      #daz-game-modal-cards {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
        gap: 6px;
        align-items: start;
      }

      .daz-game-command-row {
        border: 1px solid rgba(212, 175, 55, 0.22);
        border-radius: 6px;
        background: radial-gradient(circle at 20% 30%, rgba(22, 155, 98, 0.15) 0, transparent 30%), radial-gradient(circle at 80% 70%, rgba(255, 136, 62, 0.12) 0, transparent 25%), linear-gradient(180deg, rgba(26, 15, 8, 0.92) 0, rgba(15, 12, 10, 0.95) 100%);
        padding: 6px;
      }

      .daz-game-command-toolbar {
        display: grid;
        grid-template-columns: repeat(3, minmax(0, 1fr));
        gap: 4px;
        align-items: start;
      }

      .daz-game-command-category {
        display: flex;
        flex-direction: column;
        gap: 4px;
        align-self: start;
      }

      .daz-game-command-category > .daz-game-modal-btn {
        justify-content: space-between;
        width: 100%;
        text-transform: none;
        letter-spacing: 0.8px;
        font-size: 11px;
        padding: 0 8px;
      }

      .daz-game-command-list {
        display: none;
        grid-template-columns: repeat(auto-fit, minmax(80px, 1fr));
        gap: 4px;
        padding: 4px;
        border: 1px solid rgba(212, 175, 55, 0.25);
        border-radius: 4px;
        background: rgba(13, 7, 4, 0.75);
      }

      .daz-game-command-category.is-open .daz-game-command-list {
        display: grid;
      }

      .daz-game-command-item {
        border: 1px solid rgba(212, 175, 55, 0.28);
        border-radius: 3px;
        min-height: 20px;
        padding: 2px 6px;
        color: #c9a961;
        background: linear-gradient(180deg, rgba(26, 15, 8, 0.95), rgba(15, 12, 10, 0.96));
        font-size: 10px;
      }

      .daz-game-card {
        border: 1px solid rgba(212, 175, 55, 0.2);
        border-radius: 6px;
        background: radial-gradient(circle at 20% 30%, rgba(139, 105, 20, 0.12) 0, transparent 30%), radial-gradient(circle at 80% 70%, rgba(139, 105, 20, 0.08) 0, transparent 25%), linear-gradient(180deg, rgba(26, 15, 8, 0.9) 0, rgba(15, 12, 10, 0.95) 100%);
        padding: 6px;
        align-self: start;
      }

      .daz-game-section-title {
        margin: 0 0 5px;
        font-size: 11px;
        color: #d4af37;
        white-space: nowrap;
      }

      .daz-game-section {
        display: flex;
        flex-direction: column;
        gap: 6px;
      }

      .daz-game-section-header {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 6px;
      }

      .daz-game-section-toggle {
        border: 1px solid rgba(212, 175, 55, 0.35);
        border-radius: 3px;
        width: 20px;
        height: 20px;
        min-width: 20px;
        padding: 0;
        line-height: 1;
        color: #d4af37;
        background: linear-gradient(135deg, #2d1810 0, #1d1410 100%);
        cursor: pointer;
        box-shadow: 0 2px 5px rgba(0, 0, 0, 0.5), inset 0 1px 0 rgba(212, 175, 55, 0.1);
        transition: 0.25s;
      }

      .daz-game-section-toggle:hover {
        background: linear-gradient(135deg, #4d3830 0, #3d2820 50%, #2d1810 100%);
        color: gold;
        transform: translateY(-1px);
      }

      .daz-game-section-content {
        transition: opacity 0.2s ease, transform 0.2s ease;
        transform-origin: top;
      }

      .daz-game-card.is-collapsed .daz-game-section-content {
        display: none;
      }

      .daz-game-value-row {
        display: flex;
        justify-content: space-between;
        align-items: baseline;
        gap: 8px;
      }

      .daz-game-value-row strong {
        font-size: 14px;
        color: #d4af37;
        letter-spacing: 0.7px;
      }

      .daz-game-needs-grid {
        display: grid;
        gap: 6px;
      }

      .daz-game-need-row {
        display: grid;
        grid-template-columns: 16px 52px 1fr;
        align-items: center;
        gap: 6px;
        min-height: 14px;
      }

      .daz-game-need-emoji {
        font-size: 12px;
        line-height: 1;
        width: 16px;
        min-width: 16px;
        max-width: 16px;
        text-align: center;
        justify-self: start;
      }

      .daz-game-need-name {
        font-family: Cinzel, serif;
        font-size: 10px;
        line-height: 1;
        letter-spacing: 0.4px;
        color: #c9a961;
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
      }

      .daz-game-need-meter {
        --dazNeedFill: 0%;
        position: relative;
        width: 100%;
        min-width: 0;
        max-width: 100%;
        height: 9px;
        min-height: 9px;
        border-radius: 999px;
        border: 1px solid rgba(212, 175, 55, 0.45);
        background: rgba(13, 7, 4, 0.9);
        overflow: hidden;
      }

      .daz-game-need-meter::before {
        content: "";
        position: absolute;
        inset: 1px;
        width: var(--dazNeedFill);
        background: linear-gradient(90deg, rgba(255, 136, 62, 0.85) 0, rgba(22, 155, 98, 0.9) 100%);
        border-radius: 999px;
        transition: width 0.2s ease;
        min-width: 0;
      }

      .daz-game-need-meter:focus-visible {
        outline: 1px solid rgba(212, 175, 55, 0.8);
        outline-offset: 2px;
      }

      .daz-game-buff-list {
        display: flex;
        flex-wrap: wrap;
        gap: 3px;
        min-height: 20px;
        font-size: 10px;
        align-items: flex-start;
      }

      .daz-game-effect-dot {
        display: inline-block;
        width: 11px;
        height: 11px;
        line-height: 11px;
        text-align: center;
        border: 1px solid rgba(212, 175, 55, 0.35);
        border-radius: 50%;
        color: #d4af37;
        font-size: 10px;
        background: linear-gradient(180deg, rgba(26, 15, 8, 0.9), rgba(15, 12, 10, 0.95));
      }

      .daz-game-empty-list-space {
        color: rgba(160, 128, 80, 0.5);
        font-size: 10px;
        line-height: 1.2;
        min-height: 11px;
      }

      .daz-game-metric-note {
        margin-top: 5px;
        color: #a08050;
        font-size: 10px;
      }

      #daz-game-modal-root.daz-state-minimized {
        width: 220px !important;
        height: 38px !important;
        bottom: 12px !important;
        left: 12px !important;
        top: auto !important;
        right: auto !important;
      }

      #daz-game-modal-root.daz-state-minimized #daz-game-modal-header {
        justify-content: space-between;
        gap: 0;
      }

      #daz-game-modal-root.daz-state-minimized #daz-game-modal-hint {
        display: none !important;
      }

      #daz-game-modal-root.daz-state-minimized #daz-game-modal-body {
        display: none !important;
      }

      #daz-game-modal-root.daz-state-minimized #daz-game-modal {
        width: 100%;
        height: 38px;
      }

      #daz-game-modal-root.daz-state-minimized #daz-game-modal-resize-handle {
        display: none;
      }

      #daz-game-modal-resize-handle {
        position: absolute;
        right: 3px;
        bottom: 3px;
        width: 18px;
        height: 18px;
        z-index: 3;
        cursor: nwse-resize;
        border-right: 2px solid rgba(212, 175, 55, 0.6);
        border-bottom: 2px solid rgba(212, 175, 55, 0.6);
        border-radius: 0 0 8px 0;
        pointer-events: auto;
        touch-action: none;
        background:
          linear-gradient(135deg, transparent 0 65%, rgba(212, 175, 55, 0.5) 65%, transparent 78%, rgba(255, 255, 255, 0.28) 90%);
      }

      #daz-game-modal-resize-handle::before,
      #daz-game-modal-resize-handle::after {
        content: "";
        position: absolute;
        width: 10px;
        left: 3px;
        background: rgba(255, 255, 255, 0.25);
      }

      #daz-game-modal-resize-handle::before {
        bottom: 7px;
        height: 1px;
      }

      #daz-game-modal-resize-handle::after {
        bottom: 3px;
        height: 1px;
      }

      @media (min-width: 1600px) {
        #daz-game-modal-root {
          width: clamp(420px, 30vw, 860px);
          height: clamp(320px, 34vh, 470px);
        }
      }

      @media (max-width: 1200px) {
        #daz-game-modal-root {
          width: min(96vw, 620px);
          height: clamp(250px, 45vh, 420px);
        }
      }

      @media (max-width: 680px) {
        #daz-game-modal-root {
          left: 8px;
          bottom: 8px;
          width: calc(100vw - 10px);
          height: clamp(260px, 56vh, 430px);
        }
      }
    `,
    createMarkup: () => `
      <div id="daz-game-modal">
        <div id="daz-game-modal-header">
          <div id="daz-game-modal-header-actions">
            <button type="button" class="daz-game-modal-btn" data-action="toggle-min" title="Minimise" id="daz-game-modal-min-toggle">_</button>
          </div>
        </div>
        <div id="daz-game-modal-body">
          <div id="daz-game-modal-summary">
            <section class="daz-game-command-row">
              <div class="daz-game-command-toolbar">
                <div class="daz-game-command-category" data-command-category="games">
                  <button
                    type="button"
                    class="daz-game-modal-btn"
                    data-action="toggle-command-category"
                    data-command-category="games"
                    aria-expanded="false"
                    aria-label="Toggle game commands"
                  >
                    Games
                    <span aria-hidden="true" data-command-chevron>▸</span>
                  </button>
                  <div class="daz-game-command-list" aria-live="polite">
                    <span class="daz-game-command-item">🎣 fish</span>
                    <span class="daz-game-command-item">🔮 8ball</span>
                    <span class="daz-game-command-item">🎲 spin</span>
                    <span class="daz-game-command-item">🍺 drink</span>
                  </div>
                </div>
                <div class="daz-game-command-category" data-command-category="cmds">
                  <button
                    type="button"
                    class="daz-game-modal-btn"
                    data-action="toggle-command-category"
                    data-command-category="cmds"
                    aria-expanded="false"
                    aria-label="Toggle chat commands"
                  >
                    Cmds
                    <span aria-hidden="true" data-command-chevron>▸</span>
                  </button>
                  <div class="daz-game-command-list" aria-live="polite">
                    <span class="daz-game-command-item">!help</span>
                    <span class="daz-game-command-item">!balance</span>
                    <span class="daz-game-command-item">!shop</span>
                    <span class="daz-game-command-item">!piss</span>
                  </div>
                </div>
                <div class="daz-game-command-category" data-command-category="etc">
                  <button
                    type="button"
                    class="daz-game-modal-btn"
                    data-action="toggle-command-category"
                    data-command-category="etc"
                    aria-expanded="false"
                    aria-label="Toggle utility commands"
                  >
                    Etc
                    <span aria-hidden="true" data-command-chevron>▸</span>
                  </button>
                  <div class="daz-game-command-list" aria-live="polite">
                    <span class="daz-game-command-item">!status</span>
                    <span class="daz-game-command-item">!time</span>
                    <span class="daz-game-command-item">!tip</span>
                    <span class="daz-game-command-item">!about</span>
                  </div>
                </div>
              </div>
            </section>
            <div id="daz-game-modal-cards">
              <section class="daz-game-card daz-game-section" data-section="currency">
                <div class="daz-game-section-header">
                  <h4 class="daz-game-section-title">Currency</h4>
                  <button
                    type="button"
                    class="daz-game-modal-btn daz-game-section-toggle"
                    data-action="toggle-section"
                    data-section-key="currency"
                    aria-expanded="true"
                    aria-label="Collapse currency section"
                  >
                    ▾
                  </button>
                </div>
                <div class="daz-game-card-content daz-game-section-content" id="daz-modal-currency-content">
                  <div class="daz-game-value-row">
                    <span>Balance</span>
                    <strong id="daz-modal-balance">0</strong>
                  </div>
                  <div class="daz-game-metric-note" id="daz-modal-currency-note">Syncing live balance...</div>
                </div>
              </section>
              <section class="daz-game-card daz-game-section" data-section="needs">
                <div class="daz-game-section-header">
                  <h4 class="daz-game-section-title">Needs</h4>
                  <button
                    type="button"
                    class="daz-game-modal-btn daz-game-section-toggle"
                    data-action="toggle-section"
                    data-section-key="needs"
                    aria-expanded="true"
                    aria-label="Collapse needs section"
                  >
                    ▾
                  </button>
                </div>
                <div class="daz-game-card-content daz-game-section-content" id="daz-modal-needs-content">
                  <div class="daz-game-needs-grid">
                    <div class="daz-game-need-row">
                      <span class="daz-game-need-emoji">🍗</span>
                      <span class="daz-game-need-name">Food</span>
                      <span class="daz-game-need-meter" id="daz-modal-need-food-meter" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-label="Food"></span>
                    </div>
                    <div class="daz-game-need-row">
                      <span class="daz-game-need-emoji">🍺</span>
                      <span class="daz-game-need-name">Buzz</span>
                      <span class="daz-game-need-meter" id="daz-modal-need-alcohol-meter" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-label="Alcohol"></span>
                    </div>
                    <div class="daz-game-need-row">
                      <span class="daz-game-need-emoji">🌿</span>
                      <span class="daz-game-need-name">Weed</span>
                      <span class="daz-game-need-meter" id="daz-modal-need-weed-meter" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-label="Weed"></span>
                    </div>
                    <div class="daz-game-need-row">
                      <span class="daz-game-need-emoji">💧</span>
                      <span class="daz-game-need-name">Bladder</span>
                      <span class="daz-game-need-meter" id="daz-modal-need-bladder-meter" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-label="Bladder"></span>
                    </div>
                    <div class="daz-game-need-row">
                      <span class="daz-game-need-emoji">💘</span>
                      <span class="daz-game-need-name">Lust</span>
                      <span class="daz-game-need-meter" id="daz-modal-need-lust-meter" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-label="Lust"></span>
                    </div>
                  </div>
                </div>
              </section>
              <section class="daz-game-card daz-game-section" data-section="buffs">
                <div class="daz-game-section-header">
                  <h4 class="daz-game-section-title">Buffs &amp; Debuffs</h4>
                  <button
                    type="button"
                    class="daz-game-modal-btn daz-game-section-toggle"
                    data-action="toggle-section"
                    data-section-key="buffs"
                    aria-expanded="true"
                    aria-label="Collapse buffs and debuffs section"
                  >
                    ▾
                  </button>
                </div>
                <div class="daz-game-card-content daz-game-section-content" id="daz-modal-buffs-content">
                  <div class="daz-game-buff-list" id="daz-modal-buffs-list" aria-live="polite">
                    <span class="daz-game-empty-list-space">—</span>
                  </div>
                </div>
              </section>
            </div>
          </div>
        </div>
        <div id="daz-game-modal-resize-handle" title="Resize"></div>
      </div>
    `,
    placeholderMessageFor: () => null,
  };
})();
