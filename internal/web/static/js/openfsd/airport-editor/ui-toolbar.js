/**
 * Airport editor toolbar: Open .apt / Open .air / Fit / Layer (read-only PR).
 * DOM-facing; no model mutations beyond calling injected handlers.
 */

/**
 * @typedef {Object} ToolbarHandlers
 * @property {(file: File) => void} onOpenApt
 * @property {(file: File) => void} onOpenAir
 * @property {() => void} onFit
 * @property {() => string} onToggleLayer  returns active layer label for button
 */

/**
 * Wire toolbar controls inside root (expects data-js hooks from template).
 * @param {HTMLElement} root
 * @param {ToolbarHandlers} handlers
 * @returns {{ setLayerLabel: (label: string) => void, destroy: () => void }}
 */
export function mountToolbar(root, handlers) {
  const aptInput = root.querySelector('[data-js="open-apt"]');
  const airInput = root.querySelector('[data-js="open-air"]');
  const fitBtn = root.querySelector('[data-js="fit"]');
  const layerBtn = root.querySelector('[data-js="layer"]');

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

  /**
   * @param {string} label
   */
  function setLayerLabel(label) {
    if (!layerBtn) return;
    layerBtn.textContent = `Layer: ${label}`;
    layerBtn.setAttribute('aria-label', `Basemap layer: ${label}. Click to switch.`);
  }

  function destroy() {
    for (const [el, type, fn] of bindings) {
      el.removeEventListener(type, fn);
    }
    bindings.length = 0;
  }

  return { setLayerLabel, destroy };
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
