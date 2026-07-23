/**
 * Airport editor right rail: tabs + lists + inspector VIEW (read-only).
 * No mutation forms that write to the model.
 */

import { formatAPT } from './format-apt.js';
import { formatAIR } from './format-air.js';
import { kindShort, normalizeSelection, surfaceLabel } from './map-layers.js';
import { SurfaceRunway } from './model.js';

export const RAIL_TABS = ['airport', 'surfaces', 'aircraft', 'validate', 'raw'];

/**
 * @typedef {Object} RailHandlers
 * @property {(sel: {type:string,index:number}|null) => void} onSelect
 * @property {(tab: string) => void} [onTab]
 */

/**
 * Mount rail tab interactions on root.
 * @param {HTMLElement} root
 * @param {RailHandlers} handlers
 * @returns {{ setTab: (id: string) => void, getTab: () => string, render: (doc: import('./model.js').EditorDocument) => void, destroy: () => void }}
 */
export function mountRail(root, handlers) {
  const tablist = root.querySelector('[data-js="rail-tabs"]');
  const panels = root.querySelectorAll('[data-js-tab-panel]');
  let activeTab = 'airport';

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

  on(tablist, 'click', (ev) => {
    const t = /** @type {HTMLElement} */ (ev.target);
    const btn = t.closest('[data-tab]');
    if (!btn || !tablist.contains(btn)) return;
    const id = btn.getAttribute('data-tab');
    if (!id) return;
    setTab(id);
  });

  // List row selection (event delegation on rail body).
  const rail = root.querySelector('[data-js="rail"]') || root;
  on(rail, 'click', (ev) => {
    const t = /** @type {HTMLElement} */ (ev.target);
    const row = t.closest('[data-select-type]');
    if (!row || !rail.contains(row)) return;
    const type = row.getAttribute('data-select-type');
    const index = parseInt(row.getAttribute('data-select-index') || '', 10);
    if ((type === 'surface' || type === 'aircraft') && Number.isInteger(index)) {
      handlers.onSelect({ type, index });
    }
  });

  /**
   * @param {string} id
   */
  function setTab(id) {
    if (!RAIL_TABS.includes(id)) return;
    activeTab = id;
    if (tablist) {
      tablist.querySelectorAll('[data-tab]').forEach((btn) => {
        const on = btn.getAttribute('data-tab') === id;
        btn.setAttribute('aria-selected', on ? 'true' : 'false');
        btn.classList.toggle('is-active', on);
      });
    }
    panels.forEach((panel) => {
      const pid = panel.getAttribute('data-js-tab-panel');
      const on = pid === id;
      panel.hidden = !on;
      panel.classList.toggle('is-active', on);
    });
    if (handlers.onTab) handlers.onTab(id);
  }

  function getTab() {
    return activeTab;
  }

  /**
   * Re-render all rail panels from document.
   * @param {import('./model.js').EditorDocument} doc
   */
  function render(doc) {
    renderAirportPanel(root, doc);
    renderSurfacesPanel(root, doc);
    renderAircraftPanel(root, doc);
    renderValidatePanel(root, doc);
    renderRawPanel(root, doc);
    renderInspector(root, doc);
  }

  function destroy() {
    for (const [el, type, fn] of bindings) {
      el.removeEventListener(type, fn);
    }
    bindings.length = 0;
  }

  setTab(activeTab);
  return { setTab, getTab, render, destroy };
}

/**
 * @param {HTMLElement} root
 * @param {import('./model.js').EditorDocument} doc
 */
function renderAirportPanel(root, doc) {
  const el = root.querySelector('[data-js="panel-airport"]');
  if (!el) return;
  clearEl(el);
  const apt = doc.airport;
  if (!apt) {
    el.appendChild(hint('Open a .apt file to view airport headers.'));
    return;
  }
  const dl = document.createElement('dl');
  dl.className = 'apted-kv';
  addKV(dl, 'ICAO', apt.icao || '—');
  addKV(dl, 'Mag var', String(apt.magVar ?? 0));
  addKV(dl, 'Field elev', String(apt.fieldElev ?? 0));
  addKV(dl, 'Pattern elev', String(apt.patternElev ?? 0));
  addKV(dl, 'Pattern size', String(apt.patternSize ?? 0));
  addKV(dl, 'Climb props', String(apt.initClimbProps ?? 0));
  addKV(dl, 'Climb jets', String(apt.initClimbJets ?? 0));
  addKV(dl, 'Jet airlines', apt.jetAirlines || '—');
  addKV(dl, 'Turbo airlines', apt.turboAirlines || '—');
  addKV(dl, 'Registration', apt.registration || '—');
  addKV(dl, 'Surfaces', String(apt.surfaces?.length ?? 0));
  if (doc.aptFilename) addKV(dl, 'File', doc.aptFilename);
  el.appendChild(dl);
}

/**
 * @param {HTMLElement} root
 * @param {import('./model.js').EditorDocument} doc
 */
function renderSurfacesPanel(root, doc) {
  const el = root.querySelector('[data-js="panel-surfaces"]');
  if (!el) return;
  clearEl(el);
  const surfaces = doc.airport?.surfaces || [];
  if (!surfaces.length) {
    el.appendChild(hint('No surfaces. Open a .apt file.'));
    return;
  }
  const sel = normalizeSelection(doc.selection);
  const ul = document.createElement('ul');
  ul.className = 'apted-list';
  ul.setAttribute('role', 'listbox');
  ul.setAttribute('aria-label', 'Surfaces');
  surfaces.forEach((s, index) => {
    const li = document.createElement('li');
    li.className = 'apted-list-item';
    li.setAttribute('role', 'option');
    li.setAttribute('data-select-type', 'surface');
    li.setAttribute('data-select-index', String(index));
    li.tabIndex = 0;
    const active = sel?.type === 'surface' && sel.index === index;
    li.setAttribute('aria-selected', active ? 'true' : 'false');
    if (active) li.classList.add('is-selected');
    const pts = s.points?.length ?? 0;
    li.textContent = `${s.name || '?'}  ${kindShort(s.kind)}  ${pts} pt${pts === 1 ? '' : 's'}`;
    ul.appendChild(li);
  });
  el.appendChild(ul);
}

/**
 * @param {HTMLElement} root
 * @param {import('./model.js').EditorDocument} doc
 */
function renderAircraftPanel(root, doc) {
  const el = root.querySelector('[data-js="panel-aircraft"]');
  if (!el) return;
  clearEl(el);
  const rows = doc.aircraft || [];
  if (!rows.length) {
    el.appendChild(hint('No aircraft. Open a .air file.'));
    return;
  }
  const sel = normalizeSelection(doc.selection);
  const ul = document.createElement('ul');
  ul.className = 'apted-list';
  ul.setAttribute('role', 'listbox');
  ul.setAttribute('aria-label', 'Aircraft');
  rows.forEach((ac, index) => {
    const li = document.createElement('li');
    li.className = 'apted-list-item';
    li.setAttribute('role', 'option');
    li.setAttribute('data-select-type', 'aircraft');
    li.setAttribute('data-select-index', String(index));
    li.tabIndex = 0;
    const active = sel?.type === 'aircraft' && sel.index === index;
    li.setAttribute('aria-selected', active ? 'true' : 'false');
    if (active) li.classList.add('is-selected');
    li.textContent = `${ac.callsign || '?'}  ${ac.type || ''}  ${ac.engine || ''}  ${ac.rules || ''}  sqk ${ac.squawk || '—'}`;
    ul.appendChild(li);
  });
  el.appendChild(ul);
}

/**
 * @param {HTMLElement} root
 * @param {import('./model.js').EditorDocument} doc
 */
function renderValidatePanel(root, doc) {
  const el = root.querySelector('[data-js="panel-validate"]');
  if (!el) return;
  clearEl(el);
  const aptErrs = doc.aptErrors || [];
  const airErrs = doc.airErrors || [];
  const soft = doc.softWarnings || [];
  if (!aptErrs.length && !airErrs.length && !soft.length) {
    if (!doc.airport && !(doc.aircraft && doc.aircraft.length)) {
      el.appendChild(hint('Load .apt / .air to see parse issues.'));
    } else {
      el.appendChild(hint('No parse errors or soft warnings.'));
    }
    return;
  }
  if (aptErrs.length) {
    el.appendChild(sectionTitle(`APT errors (${aptErrs.length})`));
    el.appendChild(errorList(aptErrs));
  }
  if (airErrs.length) {
    el.appendChild(sectionTitle(`AIR errors (${airErrs.length})`));
    el.appendChild(errorList(airErrs));
  }
  if (soft.length) {
    el.appendChild(sectionTitle(`Soft warnings (${soft.length})`));
    el.appendChild(errorList(soft, 'warn'));
  }
}

/**
 * Read-only formatted raw preview.
 * @param {HTMLElement} root
 * @param {import('./model.js').EditorDocument} doc
 */
function renderRawPanel(root, doc) {
  const aptTa = root.querySelector('[data-js="raw-apt"]');
  const airTa = root.querySelector('[data-js="raw-air"]');
  if (aptTa && aptTa instanceof HTMLTextAreaElement) {
    aptTa.value = doc.airport ? formatAPT(doc.airport) : '';
    aptTa.readOnly = true;
  }
  if (airTa && airTa instanceof HTMLTextAreaElement) {
    airTa.value = doc.aircraft?.length ? formatAIR(doc.aircraft) : '';
    airTa.readOnly = true;
  }
}

/**
 * Inspector view for current selection (read-only).
 * @param {HTMLElement} root
 * @param {import('./model.js').EditorDocument} doc
 */
function renderInspector(root, doc) {
  const el = root.querySelector('[data-js="inspector"]');
  if (!el) return;
  clearEl(el);
  const sel = normalizeSelection(doc.selection);
  if (!sel) {
    el.appendChild(hint('Select a surface or aircraft on the map or in a list.'));
    return;
  }
  if (sel.type === 'surface') {
    const s = doc.airport?.surfaces?.[sel.index];
    if (!s) {
      el.appendChild(hint('Surface not found.'));
      return;
    }
    el.appendChild(sectionTitle(surfaceLabel(s)));
    const dl = document.createElement('dl');
    dl.className = 'apted-kv';
    addKV(dl, 'Kind', s.kind || '—');
    addKV(dl, 'Name', s.name || '—');
    if (s.kind === SurfaceRunway) {
      addKV(dl, 'End A', s.rwyA || '—');
      addKV(dl, 'End B', s.rwyB || '—');
      addKV(dl, 'Disp A/B', `${s.dispA ?? 0}/${s.dispB ?? 0}`);
      addKV(dl, 'Turnoff', s.turnoffLeft === false ? 'right' : 'left');
    }
    addKV(dl, 'Points', String(s.points?.length ?? 0));
    el.appendChild(dl);
    if (s.points?.length) {
      const table = document.createElement('table');
      table.className = 'apted-mini-table';
      const thead = document.createElement('thead');
      thead.innerHTML = '<tr><th>#</th><th>Lat</th><th>Lon</th></tr>';
      table.appendChild(thead);
      const tbody = document.createElement('tbody');
      s.points.forEach((p, i) => {
        const tr = document.createElement('tr');
        tr.appendChild(td(String(i + 1)));
        tr.appendChild(td(fmt6(p.lat)));
        tr.appendChild(td(fmt6(p.lon)));
        tbody.appendChild(tr);
      });
      table.appendChild(tbody);
      el.appendChild(table);
    }
    return;
  }
  if (sel.type === 'aircraft') {
    const ac = doc.aircraft?.[sel.index];
    if (!ac) {
      el.appendChild(hint('Aircraft not found.'));
      return;
    }
    el.appendChild(sectionTitle(ac.callsign || 'Aircraft'));
    const dl = document.createElement('dl');
    dl.className = 'apted-kv';
    addKV(dl, 'Callsign', ac.callsign || '—');
    addKV(dl, 'Type', ac.type || '—');
    addKV(dl, 'Engine', ac.engine || '—');
    addKV(dl, 'Rules', ac.rules || '—');
    addKV(dl, 'Dep', ac.dep || '—');
    addKV(dl, 'Arr', ac.arr || '—');
    addKV(dl, 'Cruise', String(ac.cruiseAlt ?? 0));
    addKV(dl, 'Route', ac.route || '—');
    addKV(dl, 'Remarks', ac.remarks || '—');
    addKV(dl, 'Squawk', ac.squawk || '—');
    addKV(dl, 'XPDR', ac.xpdrMode || '—');
    addKV(dl, 'Lat', fmt6(ac.lat));
    addKV(dl, 'Lon', fmt6(ac.lon));
    addKV(dl, 'Alt', String(ac.alt ?? 0));
    addKV(dl, 'Speed', String(ac.speed ?? 0));
    addKV(dl, 'Heading', String(ac.heading ?? 0));
    el.appendChild(dl);
  }
}

/**
 * @param {HTMLElement} root
 * @param {{ icao: string, apt: string, air: string, counts: string }} chips
 */
export function applyTitlebarChips(root, chips) {
  setText(root.querySelector('[data-js="chip-icao"]'), chips.icao);
  setText(root.querySelector('[data-js="chip-apt"]'), chips.apt);
  setText(root.querySelector('[data-js="chip-air"]'), chips.air);
  setText(root.querySelector('[data-js="chip-counts"]'), chips.counts);
  const aptChip = root.querySelector('[data-js="chip-apt"]');
  const airChip = root.querySelector('[data-js="chip-air"]');
  if (aptChip) {
    aptChip.classList.toggle('is-dirty', chips.apt.endsWith('*'));
    aptChip.setAttribute(
      'title',
      chips.apt.endsWith('*')
        ? 'Unsaved APT changes (download to save locally)'
        : 'Airport geometry',
    );
  }
  if (airChip) {
    airChip.classList.toggle('is-dirty', chips.air.endsWith('*'));
    airChip.setAttribute(
      'title',
      chips.air.endsWith('*')
        ? 'Unsaved AIR changes (download to save locally)'
        : 'Scenario aircraft',
    );
  }
}

// --- DOM helpers (textContent only for user-derived values) ---

/**
 * @param {HTMLElement} el
 */
function clearEl(el) {
  while (el.firstChild) el.removeChild(el.firstChild);
}

/**
 * @param {string} text
 */
function hint(text) {
  const p = document.createElement('p');
  p.className = 'apted-hint';
  p.textContent = text;
  return p;
}

/**
 * @param {string} text
 */
function sectionTitle(text) {
  const h = document.createElement('h3');
  h.className = 'apted-subhd';
  h.textContent = text;
  return h;
}

/**
 * @param {string[]} items
 * @param {'err'|'warn'} [kind='err']
 */
function errorList(items, kind = 'err') {
  const ul = document.createElement('ul');
  ul.className = kind === 'warn' ? 'apted-err-list apted-err-list-warn' : 'apted-err-list';
  for (const msg of items) {
    const li = document.createElement('li');
    li.textContent = msg;
    ul.appendChild(li);
  }
  return ul;
}

/**
 * @param {HTMLDListElement} dl
 * @param {string} k
 * @param {string} v
 */
function addKV(dl, k, v) {
  const wrap = document.createElement('div');
  wrap.className = 'apted-kv-row';
  const dt = document.createElement('dt');
  dt.textContent = k;
  const dd = document.createElement('dd');
  dd.textContent = v;
  wrap.appendChild(dt);
  wrap.appendChild(dd);
  dl.appendChild(wrap);
}

/**
 * @param {string} text
 */
function td(text) {
  const cell = document.createElement('td');
  cell.textContent = text;
  return cell;
}

/**
 * @param {number} n
 */
function fmt6(n) {
  if (!Number.isFinite(n)) return '—';
  return Number(n).toFixed(6);
}

/**
 * @param {Element|null} el
 * @param {string} text
 */
function setText(el, text) {
  if (el) el.textContent = text;
}
