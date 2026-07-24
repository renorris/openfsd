/**
 * Airport editor right rail: tabs + lists + editable inspector + Raw Apply.
 */

import { formatAPT } from './format-apt.js';
import { formatAIR } from './format-air.js';
import { kindShort, normalizeSelection, surfaceLabel } from './map-layers.js';
import {
  SurfaceRunway,
  SurfaceParking,
  SurfaceTaxiway,
  SurfaceHold,
  EnginePiston,
  EngineTurboprop,
  EngineJet,
  EngineHelicopter,
  RulesVFR,
  RulesIFR,
  RulesDVFR,
  RulesSVFR,
  XPDRModeNormal,
  XPDRModeStandby,
  hasParking,
  parkingSurfaces,
} from './model.js';

export const RAIL_TABS = ['airport', 'surfaces', 'aircraft', 'validate', 'raw'];

/**
 * @typedef {Object} RailHandlers
 * @property {(sel: {type:string,index:number}|null) => void} onSelect
 * @property {(tab: string) => void} [onTab]
 * @property {(patch: object) => void} [onAirportPatch]
 * @property {(index: number, patch: object) => void} [onSurfacePatch]
 * @property {(index: number) => void} [onDeleteSurface]
 * @property {(index: number, afterIndex: number) => void} [onInsertVertex]
 * @property {(index: number, vertexIndex: number) => void} [onDeleteVertex]
 * @property {(index: number, patch: object) => void} [onAircraftPatch]
 * @property {(index: number) => void} [onDeleteAircraft]
 * @property {(acIndex: number, parkingSurfaceIndex: number) => void} [onSnap]
 * @property {(side: 'apt'|'air', text: string) => void} [onApplyRaw]
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
  /** @type {import('./model.js').EditorDocument|null} */
  let lastDoc = null;

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

  const rail = root.querySelector('[data-js="rail"]') || root;

  /**
   * @param {Element} row
   */
  function selectRow(row) {
    if (!row || !rail.contains(row)) return;
    const type = row.getAttribute('data-select-type');
    const index = parseInt(row.getAttribute('data-select-index') || '', 10);
    if ((type === 'surface' || type === 'aircraft') && Number.isInteger(index)) {
      handlers.onSelect({ type, index });
    }
  }

  on(rail, 'click', (ev) => {
    const t = /** @type {HTMLElement} */ (ev.target);
    const row = t.closest('[data-select-type]');
    // Don't steal clicks from form controls inside inspector.
    if (t.closest('input,select,textarea,button,label')) return;
    if (row) selectRow(row);
  });

  on(rail, 'keydown', (ev) => {
    const kev = /** @type {KeyboardEvent} */ (ev);
    if (kev.key !== 'Enter' && kev.key !== ' ') return;
    const t = /** @type {HTMLElement} */ (kev.target);
    if (t.closest('input,select,textarea,button')) return;
    const row = t.closest('[data-select-type]');
    if (!row || !rail.contains(row)) return;
    kev.preventDefault();
    selectRow(row);
  });

  // Inspector + raw actions via event delegation.
  on(rail, 'click', (ev) => {
    const t = /** @type {HTMLElement} */ (ev.target);
    const btn = t.closest('[data-action]');
    if (!btn || !rail.contains(btn)) return;
    const action = btn.getAttribute('data-action');
    if (!action || !lastDoc) return;
    ev.preventDefault();
    handleAction(action, btn, lastDoc);
  });

  on(rail, 'change', (ev) => {
    const t = /** @type {HTMLElement} */ (ev.target);
    if (!(t instanceof HTMLInputElement || t instanceof HTMLSelectElement || t instanceof HTMLTextAreaElement)) {
      return;
    }
    if (!lastDoc) return;
    const field = t.getAttribute('data-field');
    const scope = t.getAttribute('data-scope');
    if (!field || !scope) return;
    applyFieldChange(scope, field, t, lastDoc);
  });

  /**
   * @param {string} action
   * @param {Element} btn
   * @param {import('./model.js').EditorDocument} doc
   */
  function handleAction(action, btn, doc) {
    const sel = normalizeSelection(doc.selection);
    if (action === 'delete-surface' && sel?.type === 'surface' && handlers.onDeleteSurface) {
      handlers.onDeleteSurface(sel.index);
      return;
    }
    if (action === 'delete-aircraft' && sel?.type === 'aircraft' && handlers.onDeleteAircraft) {
      handlers.onDeleteAircraft(sel.index);
      return;
    }
    if (action === 'insert-vertex' && sel?.type === 'surface' && handlers.onInsertVertex) {
      const s = doc.airport?.surfaces?.[sel.index];
      const after = (s?.points?.length ?? 1) - 1;
      handlers.onInsertVertex(sel.index, after);
      return;
    }
    if (action === 'delete-vertex' && sel?.type === 'surface' && handlers.onDeleteVertex) {
      const vi = parseInt(btn.getAttribute('data-vertex') || '0', 10);
      if (Number.isInteger(vi)) handlers.onDeleteVertex(sel.index, vi);
      return;
    }
    if (action === 'snap' && sel?.type === 'aircraft' && handlers.onSnap) {
      const parkIdx = parseInt(
        /** @type {HTMLSelectElement|null} */ (root.querySelector('[data-js="snap-park"]'))?.value ??
          '',
        10,
      );
      if (Number.isInteger(parkIdx)) handlers.onSnap(sel.index, parkIdx);
      return;
    }
    if (action === 'apply-raw-apt' && handlers.onApplyRaw) {
      const ta = root.querySelector('[data-js="raw-apt"]');
      if (ta instanceof HTMLTextAreaElement) handlers.onApplyRaw('apt', ta.value);
      return;
    }
    if (action === 'apply-raw-air' && handlers.onApplyRaw) {
      const ta = root.querySelector('[data-js="raw-air"]');
      if (ta instanceof HTMLTextAreaElement) handlers.onApplyRaw('air', ta.value);
      return;
    }
    if (action === 'edit-raw-apt') {
      const ta = root.querySelector('[data-js="raw-apt"]');
      if (ta instanceof HTMLTextAreaElement) {
        ta.readOnly = false;
        ta.focus();
      }
      return;
    }
    if (action === 'edit-raw-air') {
      const ta = root.querySelector('[data-js="raw-air"]');
      if (ta instanceof HTMLTextAreaElement) {
        ta.readOnly = false;
        ta.focus();
      }
      return;
    }
  }

  /**
   * @param {string} scope
   * @param {string} field
   * @param {HTMLInputElement|HTMLSelectElement|HTMLTextAreaElement} el
   * @param {import('./model.js').EditorDocument} doc
   */
  function applyFieldChange(scope, field, el, doc) {
    const val = el instanceof HTMLInputElement && el.type === 'checkbox' ? el.checked : el.value;
    if (scope === 'airport' && handlers.onAirportPatch) {
      handlers.onAirportPatch(coerceAirportField(field, val));
      return;
    }
    const sel = normalizeSelection(doc.selection);
    if (scope === 'surface' && sel?.type === 'surface' && handlers.onSurfacePatch) {
      handlers.onSurfacePatch(sel.index, coerceSurfaceField(field, val));
      return;
    }
    if (scope === 'aircraft' && sel?.type === 'aircraft' && handlers.onAircraftPatch) {
      handlers.onAircraftPatch(sel.index, coerceAircraftField(field, val));
    }
  }

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
    lastDoc = doc;
    // Preserve focus/edit state on active form control when possible.
    const active = document.activeElement;
    const keepFocus =
      active instanceof HTMLElement &&
      rail.contains(active) &&
      (active.tagName === 'INPUT' ||
        active.tagName === 'SELECT' ||
        active.tagName === 'TEXTAREA') &&
      active.getAttribute('data-field');
    const keepField = keepFocus ? active.getAttribute('data-field') : null;
    const keepScope = keepFocus ? active.getAttribute('data-scope') : null;
    const keepSel =
      keepFocus && (active instanceof HTMLInputElement || active instanceof HTMLTextAreaElement)
        ? active.selectionStart
        : null;

    renderAirportPanel(root, doc);
    renderSurfacesPanel(root, doc);
    renderAircraftPanel(root, doc);
    renderValidatePanel(root, doc);
    renderRawPanel(root, doc);
    renderInspector(root, doc);

    if (keepField && keepScope) {
      const el = root.querySelector(
        `[data-scope="${cssEscape(keepScope)}"][data-field="${cssEscape(keepField)}"]`,
      );
      if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) {
        el.focus();
        if (keepSel != null && typeof el.setSelectionRange === 'function') {
          try {
            el.setSelectionRange(keepSel, keepSel);
          } catch {
            /* ignore */
          }
        }
      } else if (el instanceof HTMLSelectElement) {
        el.focus();
      }
    }
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

/** @param {string} s */
function cssEscape(s) {
  if (typeof CSS !== 'undefined' && CSS.escape) return CSS.escape(s);
  return String(s).replace(/"/g, '\\"');
}

/**
 * @param {string} field
 * @param {*} val
 */
function coerceAirportField(field, val) {
  const numFields = new Set([
    'magVar',
    'fieldElev',
    'patternElev',
    'patternSize',
    'initClimbProps',
    'initClimbJets',
  ]);
  if (numFields.has(field)) {
    const n = Number(val);
    return { [field]: Number.isFinite(n) ? n : 0 };
  }
  if (field === 'icao') return { icao: String(val).toUpperCase().trim() };
  return { [field]: String(val) };
}

/**
 * @param {string} field
 * @param {*} val
 */
function coerceSurfaceField(field, val) {
  if (field === 'dispA' || field === 'dispB') {
    const n = Number(val);
    return { [field]: Number.isFinite(n) ? n : 0 };
  }
  if (field === 'turnoffLeft') {
    return { turnoffLeft: val === true || val === 'true' || val === 'left' };
  }
  if (field === 'rwyA' || field === 'rwyB' || field === 'name') {
    const v = String(val).toUpperCase().trim();
    const patch = { [field]: v };
    if (field === 'rwyA' || field === 'rwyB') {
      // name rebuild deferred to main when both known
    }
    return patch;
  }
  return { [field]: val };
}

/**
 * @param {string} field
 * @param {*} val
 */
function coerceAircraftField(field, val) {
  const numFields = new Set(['cruiseAlt', 'lat', 'lon', 'alt', 'speed', 'heading']);
  if (numFields.has(field)) {
    const n = Number(val);
    return { [field]: Number.isFinite(n) ? n : 0 };
  }
  if (field === 'callsign' || field === 'dep' || field === 'arr' || field === 'type') {
    return { [field]: String(val).toUpperCase().trim() };
  }
  return { [field]: String(val) };
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
    el.appendChild(
      hint('Open a .apt file or start drawing (Park/Taxi/Rwy/Hold) to create airport geometry.'),
    );
    return;
  }
  const form = document.createElement('div');
  form.className = 'apted-form-grid';
  form.appendChild(fieldInput('ICAO', 'airport', 'icao', apt.icao || '', { maxLength: 4 }));
  form.appendChild(fieldInput('Mag var', 'airport', 'magVar', String(apt.magVar ?? 0), { type: 'number', step: '0.1' }));
  form.appendChild(fieldInput('Field elev', 'airport', 'fieldElev', String(apt.fieldElev ?? 0), { type: 'number' }));
  form.appendChild(fieldInput('Pattern elev', 'airport', 'patternElev', String(apt.patternElev ?? 0), { type: 'number' }));
  form.appendChild(fieldInput('Pattern size', 'airport', 'patternSize', String(apt.patternSize ?? 0), { type: 'number', step: '0.1' }));
  form.appendChild(fieldInput('Climb props', 'airport', 'initClimbProps', String(apt.initClimbProps ?? 0), { type: 'number' }));
  form.appendChild(fieldInput('Climb jets', 'airport', 'initClimbJets', String(apt.initClimbJets ?? 0), { type: 'number' }));
  form.appendChild(fieldInput('Jet airlines', 'airport', 'jetAirlines', apt.jetAirlines || ''));
  form.appendChild(fieldInput('Turbo airlines', 'airport', 'turboAirlines', apt.turboAirlines || ''));
  form.appendChild(fieldInput('Registration', 'airport', 'registration', apt.registration || '', { maxLength: 1 }));
  if (doc.aptFilename) {
    const p = document.createElement('p');
    p.className = 'apted-hint';
    p.textContent = `File: ${doc.aptFilename}`;
    form.appendChild(p);
  }
  el.appendChild(form);
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
    el.appendChild(hint('No surfaces. Use Park / Taxi / Rwy / Hold modes to draw.'));
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
    el.appendChild(hint('No aircraft. Use Aircraft mode (6) and click the map.'));
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
      el.appendChild(hint('Load or draw geometry / aircraft to see issues.'));
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
 * Raw preview + Apply controls (not live two-way).
 * @param {HTMLElement} root
 * @param {import('./model.js').EditorDocument} doc
 */
function renderRawPanel(root, doc) {
  const aptTa = root.querySelector('[data-js="raw-apt"]');
  const airTa = root.querySelector('[data-js="raw-air"]');
  // Only rewrite when readonly (avoid clobbering in-progress Apply edit).
  if (aptTa && aptTa instanceof HTMLTextAreaElement) {
    if (aptTa.readOnly) {
      aptTa.value = doc.airport ? formatAPT(doc.airport) : '';
    }
  }
  if (airTa && airTa instanceof HTMLTextAreaElement) {
    if (airTa.readOnly) {
      airTa.value = doc.aircraft?.length ? formatAIR(doc.aircraft) : '';
    }
  }
  // Ensure action buttons exist under raw panel.
  const panel = root.querySelector('[data-js-tab-panel="raw"] .apted-panel-bd') ||
    root.querySelector('[data-js-tab-panel="raw"]');
  if (panel && !panel.querySelector('[data-action="apply-raw-apt"]')) {
    const actions = document.createElement('div');
    actions.className = 'apted-btn-row';
    actions.appendChild(actionBtn('Edit .apt', 'edit-raw-apt'));
    actions.appendChild(actionBtn('Apply .apt', 'apply-raw-apt'));
    actions.appendChild(actionBtn('Edit .air', 'edit-raw-air'));
    actions.appendChild(actionBtn('Apply .air', 'apply-raw-air'));
    panel.appendChild(actions);
    const note = document.createElement('p');
    note.className = 'apted-hint';
    note.textContent = 'Raw is a formatted preview. Edit then Apply to re-parse (not live).';
    panel.appendChild(note);
  }
}

/**
 * Editable inspector for current selection.
 * @param {HTMLElement} root
 * @param {import('./model.js').EditorDocument} doc
 */
function renderInspector(root, doc) {
  const el = root.querySelector('[data-js="inspector"]');
  if (!el) return;
  clearEl(el);
  const sel = normalizeSelection(doc.selection);
  if (!sel) {
    el.appendChild(hint('Select a surface or aircraft. Drag vertices / markers to edit.'));
    return;
  }
  if (sel.type === 'surface') {
    const s = doc.airport?.surfaces?.[sel.index];
    if (!s) {
      el.appendChild(hint('Surface not found.'));
      return;
    }
    el.appendChild(sectionTitle(surfaceLabel(s)));
    const form = document.createElement('div');
    form.className = 'apted-form-grid';
    form.appendChild(readonlyRow('Kind', s.kind || '—'));
    if (s.kind === SurfaceRunway) {
      form.appendChild(fieldInput('End A', 'surface', 'rwyA', s.rwyA || ''));
      form.appendChild(fieldInput('End B', 'surface', 'rwyB', s.rwyB || ''));
      form.appendChild(fieldInput('Disp A', 'surface', 'dispA', String(s.dispA ?? 0), { type: 'number' }));
      form.appendChild(fieldInput('Disp B', 'surface', 'dispB', String(s.dispB ?? 0), { type: 'number' }));
      form.appendChild(
        fieldSelect('Turnoff', 'surface', 'turnoffLeft', s.turnoffLeft === false ? 'right' : 'left', [
          ['left', 'Left'],
          ['right', 'Right'],
        ]),
      );
    } else {
      form.appendChild(fieldInput('Name', 'surface', 'name', s.name || ''));
    }
    el.appendChild(form);

    if (s.points?.length) {
      const table = document.createElement('table');
      table.className = 'apted-mini-table';
      const thead = document.createElement('thead');
      thead.innerHTML = '<tr><th>#</th><th>Lat</th><th>Lon</th><th></th></tr>';
      table.appendChild(thead);
      const tbody = document.createElement('tbody');
      s.points.forEach((p, i) => {
        const tr = document.createElement('tr');
        tr.appendChild(td(String(i + 1)));
        tr.appendChild(td(fmt6(p.lat)));
        tr.appendChild(td(fmt6(p.lon)));
        const act = document.createElement('td');
        const del = actionBtn('×', 'delete-vertex');
        del.setAttribute('data-vertex', String(i));
        del.title = 'Delete vertex';
        del.classList.add('apted-icon-btn');
        act.appendChild(del);
        tr.appendChild(act);
        tbody.appendChild(tr);
      });
      table.appendChild(tbody);
      el.appendChild(table);
    }

    const row = document.createElement('div');
    row.className = 'apted-btn-row';
    if (s.kind !== SurfaceParking) {
      row.appendChild(actionBtn('Add vertex', 'insert-vertex'));
    }
    row.appendChild(actionBtn('Delete surface', 'delete-surface', true));
    el.appendChild(row);
    return;
  }
  if (sel.type === 'aircraft') {
    const ac = doc.aircraft?.[sel.index];
    if (!ac) {
      el.appendChild(hint('Aircraft not found.'));
      return;
    }
    el.appendChild(sectionTitle(ac.callsign || 'Aircraft'));
    const form = document.createElement('div');
    form.className = 'apted-form-grid';
    form.appendChild(fieldInput('Callsign', 'aircraft', 'callsign', ac.callsign || ''));
    form.appendChild(fieldInput('Type', 'aircraft', 'type', ac.type || ''));
    form.appendChild(
      fieldSelect('Engine', 'aircraft', 'engine', ac.engine || EnginePiston, [
        [EnginePiston, 'Piston (P)'],
        [EngineTurboprop, 'Turboprop (T)'],
        [EngineJet, 'Jet (J)'],
        [EngineHelicopter, 'Heli (H)'],
      ]),
    );
    form.appendChild(
      fieldSelect('Rules', 'aircraft', 'rules', ac.rules || RulesVFR, [
        [RulesVFR, 'VFR (V)'],
        [RulesIFR, 'IFR (I)'],
        [RulesDVFR, 'DVFR (D)'],
        [RulesSVFR, 'SVFR (S)'],
      ]),
    );
    form.appendChild(fieldInput('Dep', 'aircraft', 'dep', ac.dep || ''));
    form.appendChild(fieldInput('Arr', 'aircraft', 'arr', ac.arr || ''));
    form.appendChild(fieldInput('Cruise', 'aircraft', 'cruiseAlt', String(ac.cruiseAlt ?? 0), { type: 'number' }));
    form.appendChild(fieldInput('Route', 'aircraft', 'route', ac.route || ''));
    form.appendChild(fieldInput('Remarks', 'aircraft', 'remarks', ac.remarks || ''));
    form.appendChild(fieldInput('Squawk', 'aircraft', 'squawk', ac.squawk || '', { maxLength: 4 }));
    form.appendChild(
      fieldSelect('XPDR', 'aircraft', 'xpdrMode', ac.xpdrMode || XPDRModeNormal, [
        [XPDRModeNormal, 'Normal (N)'],
        [XPDRModeStandby, 'Standby (S)'],
      ]),
    );
    form.appendChild(fieldInput('Lat', 'aircraft', 'lat', fmt6(ac.lat), { type: 'number', step: '0.000001' }));
    form.appendChild(fieldInput('Lon', 'aircraft', 'lon', fmt6(ac.lon), { type: 'number', step: '0.000001' }));
    form.appendChild(fieldInput('Alt', 'aircraft', 'alt', String(ac.alt ?? 0), { type: 'number' }));
    form.appendChild(fieldInput('Speed', 'aircraft', 'speed', String(ac.speed ?? 0), { type: 'number' }));
    form.appendChild(fieldInput('Heading', 'aircraft', 'heading', String(ac.heading ?? 0), { type: 'number' }));
    el.appendChild(form);

    // Snap to parking
    const parks = parkingSurfaces(doc.airport);
    const snapRow = document.createElement('div');
    snapRow.className = 'apted-btn-row';
    if (hasParking(doc.airport) && parks.length) {
      const selEl = document.createElement('select');
      selEl.setAttribute('data-js', 'snap-park');
      selEl.setAttribute('aria-label', 'Parking to snap to');
      // Map parking surface → index in airport.surfaces
      const all = doc.airport?.surfaces || [];
      for (let i = 0; i < all.length; i++) {
        if (all[i].kind !== SurfaceParking) continue;
        const opt = document.createElement('option');
        opt.value = String(i);
        opt.textContent = all[i].name || `P@${i}`;
        selEl.appendChild(opt);
      }
      snapRow.appendChild(selEl);
      snapRow.appendChild(actionBtn('Snap to parking', 'snap'));
    } else {
      const b = actionBtn('Snap to parking', 'snap');
      b.disabled = true;
      b.title = 'Load or create parking first';
      snapRow.appendChild(b);
    }
    snapRow.appendChild(actionBtn('Delete aircraft', 'delete-aircraft', true));
    el.appendChild(snapRow);
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
 * @param {string} label
 * @param {string} value
 */
function readonlyRow(label, value) {
  const wrap = document.createElement('div');
  wrap.className = 'apted-field apted-field-inline';
  const lab = document.createElement('label');
  lab.textContent = label;
  const span = document.createElement('span');
  span.className = 'apted-mono';
  span.textContent = value;
  wrap.appendChild(lab);
  wrap.appendChild(span);
  return wrap;
}

/**
 * @param {string} label
 * @param {string} scope
 * @param {string} field
 * @param {string} value
 * @param {{ type?: string, step?: string, maxLength?: number }} [opts]
 */
function fieldInput(label, scope, field, value, opts = {}) {
  const wrap = document.createElement('div');
  wrap.className = 'apted-field apted-field-inline';
  const lab = document.createElement('label');
  lab.textContent = label;
  const input = document.createElement('input');
  input.type = opts.type || 'text';
  input.value = value;
  input.setAttribute('data-scope', scope);
  input.setAttribute('data-field', field);
  input.autocomplete = 'off';
  input.spellcheck = false;
  if (opts.step) input.step = opts.step;
  if (opts.maxLength) input.maxLength = opts.maxLength;
  wrap.appendChild(lab);
  wrap.appendChild(input);
  return wrap;
}

/**
 * @param {string} label
 * @param {string} scope
 * @param {string} field
 * @param {string} value
 * @param {Array<[string,string]>} options
 */
function fieldSelect(label, scope, field, value, options) {
  const wrap = document.createElement('div');
  wrap.className = 'apted-field apted-field-inline';
  const lab = document.createElement('label');
  lab.textContent = label;
  const sel = document.createElement('select');
  sel.setAttribute('data-scope', scope);
  sel.setAttribute('data-field', field);
  for (const [v, t] of options) {
    const opt = document.createElement('option');
    opt.value = v;
    opt.textContent = t;
    if (v === value) opt.selected = true;
    sel.appendChild(opt);
  }
  wrap.appendChild(lab);
  wrap.appendChild(sel);
  return wrap;
}

/**
 * @param {string} label
 * @param {string} action
 * @param {boolean} [danger]
 */
function actionBtn(label, action, danger = false) {
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = danger
    ? 'btn btn-sm btn-outline-danger'
    : 'btn btn-sm btn-outline-secondary';
  btn.setAttribute('data-action', action);
  btn.textContent = label;
  return btn;
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

// Silence unused import lint for SurfaceTaxiway/Hold if tree-shaken poorly
void SurfaceTaxiway;
void SurfaceHold;
