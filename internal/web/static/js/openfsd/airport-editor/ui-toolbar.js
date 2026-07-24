/**
 * Airport editor toolbar: Open / Download / New / Fit / Layer / modes / Mark clean.
 */

import {
  EDITOR_MODES,
  MODE_SELECT,
  MODE_PARK,
  MODE_TAXI,
  MODE_RUNWAY,
  MODE_HOLD,
  MODE_AIRCRAFT,
} from './model.js';

/**
 * @typedef {Object} ToolbarHandlers
 * @property {(file: File) => void} onOpenApt
 * @property {(file: File) => void} onOpenAir
 * @property {() => void} onFit
 * @property {() => string} onToggleLayer  returns active layer label for button
 * @property {() => void} [onDownloadApt]
 * @property {() => void} [onDownloadAir]
 * @property {() => void} [onNew]
 * @property {() => void} [onMarkClean]
 * @property {(mode: string) => void} [onMode]
 */

const MODE_LABELS = {
  [MODE_SELECT]: 'Select',
  [MODE_PARK]: 'Park',
  [MODE_TAXI]: 'Taxi',
  [MODE_RUNWAY]: 'Rwy',
  [MODE_HOLD]: 'Hold',
  [MODE_AIRCRAFT]: 'Aircraft',
};

/**
 * Wire toolbar controls inside root (expects data-js hooks from template).
 * @param {HTMLElement} root
 * @param {ToolbarHandlers} handlers
 * @returns {{
 *   setLayerLabel: (label: string) => void,
 *   setMode: (mode: string) => void,
 *   getMode: () => string,
 *   destroy: () => void
 * }}
 */
export function mountToolbar(root, handlers) {
  const aptInput = root.querySelector('[data-js="open-apt"]');
  const airInput = root.querySelector('[data-js="open-air"]');
  const fitBtn = root.querySelector('[data-js="fit"]');
  const layerBtn = root.querySelector('[data-js="layer"]');
  const dlAptBtn = root.querySelector('[data-js="dl-apt"]');
  const dlAirBtn = root.querySelector('[data-js="dl-air"]');
  const newBtn = root.querySelector('[data-js="new"]');
  const cleanBtn = root.querySelector('[data-js="mark-clean"]');
  const modeGroup = root.querySelector('[data-js="mode-group"]');

  let activeMode = MODE_SELECT;

  /**
   * @param {Event} ev
   * @param {(f: File) => void} cb
   */
  function onFileChange(ev, cb) {
    const input = /** @type {HTMLInputElement} */ (ev.target);
    const file = input.files && input.files[0];
    // Reset so re-opening the same file fires change again.
    input.value = '';
    if (file) cb(file);
  }

  /** @type {Array<[EventTarget, string, EventListener]>} */
  const bindings = [];

  /**
   * @param {EventTarget|null} el
   * @param {string} type
   * @param {EventListener} fn
   */
  function on(el, type, fn) {
    if (!el) return;
    el.addEventListener(type, fn);
    bindings.push([el, type, fn]);
  }

  on(aptInput, 'change', (ev) => onFileChange(ev, handlers.onOpenApt));
  on(airInput, 'change', (ev) => onFileChange(ev, handlers.onOpenAir));
  on(fitBtn, 'click', (ev) => {
    ev.preventDefault();
    handlers.onFit();
  });
  on(layerBtn, 'click', (ev) => {
    ev.preventDefault();
    const label = handlers.onToggleLayer();
    setLayerLabel(label);
  });
  on(dlAptBtn, 'click', (ev) => {
    ev.preventDefault();
    if (handlers.onDownloadApt) handlers.onDownloadApt();
  });
  on(dlAirBtn, 'click', (ev) => {
    ev.preventDefault();
    if (handlers.onDownloadAir) handlers.onDownloadAir();
  });
  on(newBtn, 'click', (ev) => {
    ev.preventDefault();
    if (handlers.onNew) handlers.onNew();
  });
  on(cleanBtn, 'click', (ev) => {
    ev.preventDefault();
    if (handlers.onMarkClean) handlers.onMarkClean();
  });

  // Mode radio group (buttons with data-mode).
  if (modeGroup) {
    // Ensure mode buttons exist if template only has container.
    ensureModeButtons(modeGroup);
    on(modeGroup, 'click', (ev) => {
      const t = /** @type {HTMLElement} */ (ev.target);
      const btn = t.closest('[data-mode]');
      if (!btn || !modeGroup.contains(btn)) return;
      ev.preventDefault();
      const mode = btn.getAttribute('data-mode');
      if (!mode || !EDITOR_MODES.includes(mode)) return;
      setMode(mode);
      if (handlers.onMode) handlers.onMode(mode);
    });
  }

  /**
   * @param {string} label
   */
  function setLayerLabel(label) {
    if (!layerBtn) return;
    layerBtn.textContent = `Layer: ${label}`;
    layerBtn.setAttribute('aria-label', `Basemap layer: ${label}. Click to switch.`);
  }

  /**
   * @param {string} mode
   */
  function setMode(mode) {
    if (!EDITOR_MODES.includes(mode)) return;
    activeMode = mode;
    if (!modeGroup) return;
    modeGroup.querySelectorAll('[data-mode]').forEach((btn) => {
      const on = btn.getAttribute('data-mode') === mode;
      btn.classList.toggle('is-active', on);
      btn.setAttribute('aria-pressed', on ? 'true' : 'false');
    });
  }

  function getMode() {
    return activeMode;
  }

  function destroy() {
    for (const [el, type, fn] of bindings) {
      el.removeEventListener(type, fn);
    }
    bindings.length = 0;
  }

  setMode(activeMode);
  return { setLayerLabel, setMode, getMode, destroy };
}

/**
 * Inject mode buttons if container is empty.
 * @param {Element} group
 */
function ensureModeButtons(group) {
  if (group.querySelector('[data-mode]')) return;
  const modes = [
    [MODE_SELECT, '1 Select'],
    [MODE_PARK, '2 Park'],
    [MODE_TAXI, '3 Taxi'],
    [MODE_RUNWAY, '4 Rwy'],
    [MODE_HOLD, '5 Hold'],
    [MODE_AIRCRAFT, '6 Acft'],
  ];
  for (const [mode, label] of modes) {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'btn btn-sm btn-outline-dark apted-mode-btn';
    btn.setAttribute('data-mode', mode);
    btn.setAttribute('aria-pressed', mode === MODE_SELECT ? 'true' : 'false');
    btn.title = `${MODE_LABELS[mode]} mode (key ${modes.findIndex((m) => m[0] === mode) + 1})`;
    btn.textContent = label;
    group.appendChild(btn);
  }
}

/**
 * Read a File as text via FileReader.
 * @param {File} file
 * @returns {Promise<string>}
 */
export function readFileAsText(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result ?? ''));
    reader.onerror = () => reject(reader.error || new Error('FileReader failed'));
    reader.readAsText(file);
  });
}

/**
 * Mode key (1–6) → mode id.
 * @param {string} key
 * @returns {string|null}
 */
export function modeFromDigitKey(key) {
  const map = {
    '1': MODE_SELECT,
    '2': MODE_PARK,
    '3': MODE_TAXI,
    '4': MODE_RUNWAY,
    '5': MODE_HOLD,
    '6': MODE_AIRCRAFT,
  };
  return map[key] || null;
}
