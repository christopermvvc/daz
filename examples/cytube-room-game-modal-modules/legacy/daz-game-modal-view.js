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
        justify-content: space-between;
        padding: 0 8px;
        position: relative;
        z-index: 2;
        cursor: move;
        touch-action: none;
        user-select: none;
      }

      #daz-game-modal-title {
        font-family: "Irish Grover", cursive;
        font-size: 16px;
        color: #d4af37;
        text-shadow: 1px 1px 2px #000;
        line-height: 1.1;
        white-space: nowrap;
      }

      #daz-game-modal-hint {
        margin-left: 6px;
        font-size: 9px;
        color: rgba(212, 175, 55, 0.78);
        text-transform: none;
        letter-spacing: 0.2px;
        opacity: 0.85;
      }

      #daz-game-modal-header-actions {
        display: flex;
        gap: 4px;
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
        display: grid;
        grid-template-columns: minmax(190px, 1fr) minmax(190px, 1fr);
        gap: 6px;
        font-size: 11px;
        color: #a08050;
      }

      .daz-game-card {
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
        gap: 5px;
      }

      .daz-game-need-row {
        display: flex;
        flex-direction: column;
        gap: 3px;
      }

      .daz-game-need-top {
        display: flex;
        justify-content: space-between;
      }

      .daz-game-need-name {
        color: #a08050;
      }

      .daz-game-need-value {
        color: #d4af37;
      }

      .daz-game-need-meter {
        width: 100%;
        height: 8px;
        border: 1px solid rgba(212, 175, 55, 0.25);
        background: rgba(10, 6, 4, 0.6);
      }

      .daz-game-need-meter > i {
        display: block;
        height: 100%;
        width: 0%;
        background: linear-gradient(90deg, #ff883e 0, #d4af37 60%, #f5d37a 100%);
        transition: width 0.2s ease;
      }

      #daz-game-modal-actions {
        display: grid;
        grid-template-columns: repeat(3, minmax(0, 1fr));
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
        font-size: 11px;
        padding: 0 8px;
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

      #daz-game-modal-card-actions .daz-game-action-btn {
        font-size: 10px;
        padding: 4px 6px;
      }

      #daz-game-modal-log-wrap {
        padding: 2px 0 0;
        flex: 1;
        min-height: 0;
      }

      #daz-game-modal-log {
        height: 100%;
        min-height: 80px;
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

      #daz-game-modal-log-wrap .daz-game-section-title {
        margin: 0 0 4px;
      }

      .daz-game-action-grid {
        display: grid;
        grid-template-columns: repeat(3, minmax(0, 1fr));
        gap: 6px;
      }

      #daz-game-modal-card-actions {
        margin-top: 6px;
      }

      .daz-log-entry {
        margin-bottom: 4px;
        color: #c9a961;
      }

      .daz-log-entry strong {
        color: #d4af37;
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

      #daz-game-modal-root.daz-state-minimized #daz-game-modal-title,
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
          <div class="daz-game-modal-has-content">
            <div id="daz-game-modal-title">Paddy's Pub Game Console</div>
            <div id="daz-game-modal-hint">Drag • Resize</div>
          </div>
          <div id="daz-game-modal-header-actions">
            <button type="button" class="daz-game-modal-btn" data-action="toggle-min" title="Minimise" id="daz-game-modal-min-toggle">_</button>
          </div>
        </div>
        <div id="daz-game-modal-body">
          <div id="daz-game-modal-summary">
            <section class="daz-game-card">
              <h4 class="daz-game-section-title">Treasury</h4>
              <div class="daz-game-value-row">
                <span>Balance</span>
                <strong id="daz-modal-balance">0</strong>
              </div>
              <div id="daz-game-modal-card-actions" class="daz-game-action-grid">
                <button type="button" class="daz-game-action-btn secondary" data-action="money-add">+50</button>
                <button type="button" class="daz-game-action-btn secondary" data-action="money-spend">-25</button>
                <button type="button" class="daz-game-action-btn secondary" data-action="needs-recover">Top up needs</button>
              </div>
            </section>
            <section class="daz-game-card">
              <h4 class="daz-game-section-title">Needs</h4>
              <div class="daz-game-needs-grid">
                <div class="daz-game-need-row">
                  <div class="daz-game-need-top">
                    <span class="daz-game-need-name">Bladder</span>
                    <strong class="daz-game-need-value" id="daz-modal-need-bladder">0</strong>
                  </div>
                  <div class="daz-game-need-meter"><i id="daz-modal-need-bladder-bar"></i></div>
                </div>
                <div class="daz-game-need-row">
                  <div class="daz-game-need-top">
                    <span class="daz-game-need-name">Alcohol</span>
                    <strong class="daz-game-need-value" id="daz-modal-need-alcohol">0</strong>
                  </div>
                  <div class="daz-game-need-meter"><i id="daz-modal-need-alcohol-bar"></i></div>
                </div>
                <div class="daz-game-need-row">
                  <div class="daz-game-need-top">
                    <span class="daz-game-need-name">Weed</span>
                    <strong class="daz-game-need-value" id="daz-modal-need-weed">0</strong>
                  </div>
                  <div class="daz-game-need-meter"><i id="daz-modal-need-weed-bar"></i></div>
                </div>
                <div class="daz-game-need-row">
                  <div class="daz-game-need-top">
                    <span class="daz-game-need-name">Food</span>
                    <strong class="daz-game-need-value" id="daz-modal-need-food">0</strong>
                  </div>
                  <div class="daz-game-need-meter"><i id="daz-modal-need-food-bar"></i></div>
                </div>
                <div class="daz-game-need-row">
                  <div class="daz-game-need-top">
                    <span class="daz-game-need-name">Lust</span>
                    <strong class="daz-game-need-value" id="daz-modal-need-lust">0</strong>
                  </div>
                  <div class="daz-game-need-meter"><i id="daz-modal-need-lust-bar"></i></div>
                </div>
              </div>
            </section>
          </div>
          <div id="daz-game-modal-actions">
            <button type="button" class="daz-game-action-btn" data-action="fish">Fish cmd</button>
            <button type="button" class="daz-game-action-btn secondary" data-action="eightball">8-ball cmd</button>
            <button type="button" class="daz-game-action-btn secondary" data-action="piss">Piss cmd</button>
            <button type="button" class="daz-game-action-btn secondary" data-action="need-bathroom">Need: Bladder</button>
            <button type="button" class="daz-game-action-btn secondary" data-action="need-eat">Need: Food</button>
            <button type="button" class="daz-game-action-btn secondary" data-action="need-drink">Need: Alcohol</button>
            <button type="button" class="daz-game-action-btn secondary" data-action="need-weed">Need: Weed</button>
            <button type="button" class="daz-game-action-btn secondary" data-action="need-lust">Need: Lust</button>
          </div>
          <div id="daz-game-modal-log-wrap">
            <div class="daz-game-section-title">Sub-chat placeholder</div>
            <div id="daz-game-modal-log" aria-live="polite"></div>
          </div>
        </div>
        <div id="daz-game-modal-resize-handle" title="Resize"></div>
      </div>
    `,
    placeholderMessageFor: (action) => ({
      'money-add': 'Placeholder: deposit command path for currency',
      'money-spend': 'Placeholder: spend command path for currency',
      fish: 'Placeholder: fish command payload',
      eightball: 'Placeholder: 8-ball command payload',
      piss: 'Placeholder: piss command payload',
      'need-bathroom': 'Placeholder: bathroom / bladder placeholder',
      'need-eat': 'Placeholder: food / metabolism placeholder',
      'need-drink': 'Placeholder: drink / alcohol placeholder',
      'need-weed': 'Placeholder: weed placeholder',
      'need-lust': 'Placeholder: lust placeholder',
      'needs-recover': 'Placeholder: needs recovery placeholder',
    }[action] || null),
  };
})();
