/**
 * Airport editor bootstrap — full edit loop (PR7).
 *
 * Modes, draw, vertex/aircraft drag, Blob download, dirty hash,
 * beforeunload, replace confirms, Raw Apply, shortcuts 1–6 / Del / Esc.
 *
 * Progressive enhancement: requires Leaflet (global L) + this module.
 * Without JS: textareas + echo-download forms still work.
 */

import {
  createEmptyDocument,
  setAirport,
  setAircraft,
  setMode,
  markClean,
  resetDocument,
  setVertex,
  insertVertex,
  deleteVertex,
  deleteSurface,
  deleteAircraft,
  updateSurface,
  updateAircraft,
  updateAirportHeaders,
  snapAircraftToParking,
  MODE_SELECT,
  MODE_PARK,
  MODE_TAXI,
  MODE_RUNWAY,
  MODE_HOLD,
  MODE_AIRCRAFT,
  SurfaceRunway,
  SurfaceTaxiway,
  SurfaceHold,
  SurfaceParking,
} from './model.js';
import { parseAPT } from './parse-apt.js';
import { parseAIR } from './parse-air.js';
import { formatAPT } from './format-apt.js';
import { formatAIR } from './format-air.js';
import { validateDocument } from './validate.js';
import {
  createMap,
  OverlayController,
  titlebarChips,
  normalizeSelection,
} from './map-layers.js';
import { mountToolbar, readFileAsText, modeFromDigitKey } from './ui-toolbar.js';
import { mountRail, applyTitlebarChips } from './ui-rail.js';
import { downloadApt, downloadAir, sanitizeFilename } from './download.js';
import {
  createDrawSession,
  beginDraw,
  handleMapClick,
  finishDraw,
  escapeDraw,
  cancelDraw,
  isPolylineMode,
  modeToKind,
} from './draw-tools.js';

/**
 * @returns {void}
 */
function main() {
  const root = document.getElementById('apted-root');
  if (!root || root.getAttribute('data-js') !== 'airport-editor') return;

  if (typeof L === 'undefined') {
    // Leaflet failed to load; leave no-JS fallback visible.
    return;
  }

  const mapEl = document.getElementById('apted-map');
  if (!mapEl) return;

  root.classList.add('is-enhanced');
  const placeholder = mapEl.querySelector('.apted-map-placeholder');
  if (placeholder) placeholder.remove();

  /** @type {import('./model.js').EditorDocument} */
  const doc = createEmptyDocument();
  const draw = createDrawSession();

  const blankTiles = root.getAttribute('data-test-tiles') === 'blank';
  const mapCtl = createMap(L, mapEl, { blankTiles });
  /** @type {string} */
  let activeBase = mapCtl.activeBase; // 'osm' | 'esri' | 'blank'

  // Double-click finish flag (map fires click then dblclick).
  let suppressNextClick = false;

  const overlays = new OverlayController(L, mapCtl.map, {
    planeIconUrl: '/static/images/plane.png',
    onSelect(sel) {
      if (doc.mode !== MODE_SELECT) return;
      doc.selection = normalizeSelection(sel);
      refresh();
    },
    onVertexDrag(si, vi, lat, lon) {
      setVertex(doc, si, vi, { lat, lon });
      // Live-update surface polyline without full refresh of handles mid-drag.
      // Full refresh on dragend.
    },
    onVertexDragEnd(si, vi, lat, lon) {
      setVertex(doc, si, vi, { lat, lon });
      afterAptMutation();
    },
    onAircraftDrag(index, lat, lon) {
      updateAircraft(doc, index, { lat, lon });
    },
    onAircraftDragEnd(index, lat, lon) {
      updateAircraft(doc, index, { lat, lon });
      afterAirMutation();
    },
    onMapClick(lat, lon, originalEvent) {
      if (suppressNextClick) {
        suppressNextClick = false;
        return;
      }
      // In select mode, empty-map click clears selection.
      if (doc.mode === MODE_SELECT) {
        if (doc.selection) {
          doc.selection = null;
          refresh();
        }
        return;
      }
      const result = handleMapClick(doc, draw, { lat, lon });
      if (result.action === 'error') {
        showStatus(result.error || 'Draw error', true);
        return;
      }
      if (result.action === 'placed-park') {
        afterAptMutation({ fit: false });
        rail.setTab('surfaces');
        showStatus(result.message || 'Parking added.', false);
        return;
      }
      if (result.action === 'placed-aircraft') {
        afterAirMutation({ fit: false });
        rail.setTab('aircraft');
        showStatus(result.message || 'Aircraft placed.', false);
        return;
      }
      if (result.action === 'vertex' || result.action === 'need-more') {
        overlays.setDrawPreview(draw.points, modeToKind(doc.mode) || SurfaceTaxiway);
        showStatus(result.message || '', false);
        return;
      }
      void originalEvent;
    },
    onMapDblClick() {
      if (!isPolylineMode(doc.mode)) return;
      suppressNextClick = true;
      // Leaflet may have already added a point via the first click of the double-click.
      // Finish with current points.
      doFinishDraw();
    },
  });

  // Disable double-click zoom while drawing polylines (we use dblclick to finish).
  mapCtl.map.doubleClickZoom.disable();

  const rail = mountRail(root, {
    onSelect(sel) {
      doc.selection = normalizeSelection(sel);
      if (doc.mode !== MODE_SELECT) {
        setMode(doc, MODE_SELECT);
        toolbar.setMode(MODE_SELECT);
        cancelDraw(draw);
        overlays.clearDrawPreview();
      }
      refresh();
    },
    onAirportPatch(patch) {
      updateAirportHeaders(doc, patch);
      afterAptMutation();
    },
    onSurfacePatch(index, patch) {
      // Rebuild runway name when ends change.
      const s = doc.airport?.surfaces?.[index];
      if (s && s.kind === SurfaceRunway) {
        const next = { ...patch };
        if (next.rwyA !== undefined || next.rwyB !== undefined) {
          const a = next.rwyA !== undefined ? next.rwyA : s.rwyA;
          const b = next.rwyB !== undefined ? next.rwyB : s.rwyB;
          next.name = `${a}/${b}`;
        }
        updateSurface(doc, index, next);
      } else {
        updateSurface(doc, index, patch);
      }
      afterAptMutation();
    },
    onDeleteSurface(index) {
      const s = doc.airport?.surfaces?.[index];
      const label = s?.name || 'surface';
      if (s && (s.points?.length ?? 0) > 1) {
        if (!window.confirm(`Delete surface ${label}?`)) return;
      }
      deleteSurface(doc, index);
      afterAptMutation();
      showStatus(`Deleted ${label}.`, false);
    },
    onInsertVertex(index, afterIndex) {
      const s = doc.airport?.surfaces?.[index];
      if (!s?.points?.length) return;
      const a = s.points[Math.max(0, afterIndex)] || s.points[0];
      const b = s.points[Math.min(s.points.length - 1, afterIndex + 1)] || a;
      const mid = { lat: (a.lat + b.lat) / 2, lon: (a.lon + b.lon) / 2 };
      insertVertex(doc, index, afterIndex, mid);
      afterAptMutation();
    },
    onDeleteVertex(index, vertexIndex) {
      const res = deleteVertex(doc, index, vertexIndex);
      if (!res.ok) {
        showStatus(res.reason || 'Cannot delete vertex', true);
        return;
      }
      afterAptMutation();
    },
    onAircraftPatch(index, patch) {
      updateAircraft(doc, index, patch);
      afterAirMutation();
    },
    onDeleteAircraft(index) {
      const ac = doc.aircraft?.[index];
      const cs = ac?.callsign || 'aircraft';
      if (!window.confirm(`Delete aircraft ${cs}?`)) return;
      deleteAircraft(doc, index);
      afterAirMutation();
      showStatus(`Deleted ${cs}.`, false);
    },
    onSnap(acIndex, parkIdx) {
      if (!snapAircraftToParking(doc, acIndex, parkIdx)) {
        showStatus('Snap failed — check parking selection.', true);
        return;
      }
      afterAirMutation();
      showStatus('Snapped aircraft to parking.', false);
    },
    onApplyRaw(side, text) {
      if (side === 'apt') {
        const { airport, errors } = parseAPT(text);
        setAirport(doc, airport, { errors, markDirty: true });
        doc.selection = null;
        validateDocument(doc);
        afterAptMutation({ fit: true });
        // Restore readonly after apply
        const ta = root.querySelector('[data-js="raw-apt"]');
        if (ta instanceof HTMLTextAreaElement) ta.readOnly = true;
        showStatus(
          errors.length
            ? `Applied raw .apt with ${errors.length} issue(s).`
            : 'Applied raw .apt.',
          errors.length > 0,
        );
      } else {
        const { aircraft, errors } = parseAIR(text);
        setAircraft(doc, aircraft, { errors, markDirty: true });
        doc.selection = null;
        validateDocument(doc);
        afterAirMutation({ fit: true });
        const ta = root.querySelector('[data-js="raw-air"]');
        if (ta instanceof HTMLTextAreaElement) ta.readOnly = true;
        showStatus(
          errors.length
            ? `Applied raw .air with ${errors.length} issue(s).`
            : 'Applied raw .air.',
          errors.length > 0,
        );
      }
    },
  });

  const toolbar = mountToolbar(root, {
    async onOpenApt(file) {
      if (doc.airport && doc.aptDirty) {
        if (!window.confirm('Replace airport geometry? Unsaved APT changes will be lost.')) {
          return;
        }
      }
      try {
        const text = await readFileAsText(file);
        const { airport, errors } = parseAPT(text);
        setAirport(doc, airport, {
          filename: file.name || 'airport.apt',
          errors,
          markDirty: false,
        });
        doc.lastAptDownloadHash = null;
        syncFallbackTextareas();
        validateDocument(doc);
        doc.selection = null;
        rail.setTab('surfaces');
        refresh({ fit: true });
        showStatus(
          errors.length
            ? `Loaded ${file.name} with ${errors.length} parse issue(s).`
            : `Loaded ${file.name}.`,
          errors.length > 0,
        );
      } catch (err) {
        showStatus(`Failed to open .apt: ${errMessage(err)}`, true);
      }
    },
    async onOpenAir(file) {
      if (doc.aircraft?.length && doc.airDirty) {
        if (!window.confirm('Replace aircraft list? Unsaved AIR changes will be lost.')) {
          return;
        }
      }
      try {
        const text = await readFileAsText(file);
        const { aircraft, errors } = parseAIR(text);
        setAircraft(doc, aircraft, {
          filename: file.name || 'scenario.air',
          errors,
          markDirty: false,
        });
        doc.lastAirDownloadHash = null;
        syncFallbackTextareas();
        validateDocument(doc);
        doc.selection = null;
        rail.setTab('aircraft');
        refresh({ fit: true });
        showStatus(
          errors.length
            ? `Loaded ${file.name} with ${errors.length} parse issue(s).`
            : `Loaded ${file.name}.`,
          errors.length > 0,
        );
      } catch (err) {
        showStatus(`Failed to open .air: ${errMessage(err)}`, true);
      }
    },
    onFit() {
      const ok = overlays.fitBounds();
      if (!ok) showStatus('Nothing to fit — open or draw geometry first.', false);
    },
    onToggleLayer() {
      if (blankTiles) {
        return 'Blank';
      }
      if (activeBase === 'osm') {
        mapCtl.setBase('esri');
        activeBase = 'esri';
        return 'Esri';
      }
      mapCtl.setBase('osm');
      activeBase = 'osm';
      return 'OSM';
    },
    onDownloadApt() {
      const res = downloadApt(doc);
      if (res.empty) {
        showStatus('Nothing to download — no airport geometry.', true);
        return;
      }
      if (!res.ok) {
        showStatus('Download failed (browser blocked Blob?).', true);
        return;
      }
      syncFallbackTextareas();
      refresh();
      showStatus(`Downloading ${res.filename}…`, false);
    },
    onDownloadAir() {
      const res = downloadAir(doc);
      if (res.empty) {
        showStatus('Nothing to download — no aircraft.', true);
        return;
      }
      if (!res.ok) {
        showStatus('Download failed (browser blocked Blob?).', true);
        return;
      }
      syncFallbackTextareas();
      refresh();
      showStatus(`Downloading ${res.filename}…`, false);
    },
    onNew() {
      if (doc.aptDirty || doc.airDirty) {
        if (!window.confirm('Discard unsaved APT/AIR changes and start new?')) return;
      }
      resetDocument(doc);
      cancelDraw(draw);
      overlays.clearDrawPreview();
      toolbar.setMode(MODE_SELECT);
      rail.setTab('airport');
      mapCtl.map.setView([30, 0], 2);
      syncFallbackTextareas();
      refresh();
      showStatus('New empty document.', false);
    },
    onMarkClean() {
      markClean(doc, 'both');
      refresh();
      showStatus('Marked clean (not saved to disk).', false);
    },
    onMode(mode) {
      setMode(doc, mode);
      cancelDraw(draw);
      overlays.clearDrawPreview();
      if (isPolylineMode(mode)) {
        beginDraw(draw, mode);
      } else if (mode === MODE_PARK || mode === MODE_AIRCRAFT) {
        beginDraw(draw, mode);
      }
      refresh();
      const tips = {
        [MODE_SELECT]: 'Select mode — click features; drag vertices.',
        [MODE_PARK]: 'Park mode — click map to place parking.',
        [MODE_TAXI]: 'Taxi mode — click vertices; Enter/double-click finish.',
        [MODE_RUNWAY]: 'Runway mode — click ≥2 points; Enter/double-click finish.',
        [MODE_HOLD]: 'Hold mode — click point(s); Enter/double-click finish.',
        [MODE_AIRCRAFT]: 'Aircraft mode — click map to place (dep/arr = ICAO).',
      };
      showStatus(tips[mode] || '', false);
    },
  });

  toolbar.setLayerLabel(blankTiles ? 'Blank' : 'OSM');

  // Keyboard shortcuts
  window.addEventListener('keydown', onKeyDown);
  window.addEventListener('beforeunload', onBeforeUnload);

  requestAnimationFrame(() => {
    mapCtl.map.invalidateSize();
  });
  window.addEventListener('resize', () => {
    mapCtl.map.invalidateSize();
  });

  /**
   * @param {KeyboardEvent} ev
   */
  function onKeyDown(ev) {
    const t = /** @type {HTMLElement} */ (ev.target);
    const inField =
      t &&
      (t.tagName === 'INPUT' ||
        t.tagName === 'TEXTAREA' ||
        t.tagName === 'SELECT' ||
        t.isContentEditable);

    // Ctrl/Cmd combos work even in fields.
    if ((ev.ctrlKey || ev.metaKey) && !ev.altKey) {
      if (ev.key === 's' || ev.key === 'S') {
        ev.preventDefault();
        downloadDirtySides();
        return;
      }
      if (ev.key === 'o' || ev.key === 'O') {
        // Focus open inputs
        if (ev.shiftKey) {
          const air = root.querySelector('[data-js="open-air"]');
          if (air instanceof HTMLInputElement) air.click();
        } else {
          const apt = root.querySelector('[data-js="open-apt"]');
          if (apt instanceof HTMLInputElement) apt.click();
        }
        ev.preventDefault();
        return;
      }
    }

    if (inField) return;

    // Mode digits 1–6
    const mode = modeFromDigitKey(ev.key);
    if (mode) {
      ev.preventDefault();
      toolbar.setMode(mode);
      setMode(doc, mode);
      cancelDraw(draw);
      overlays.clearDrawPreview();
      if (isPolylineMode(mode) || mode === MODE_PARK || mode === MODE_AIRCRAFT) {
        beginDraw(draw, mode);
      }
      refresh();
      return;
    }

    if (ev.key === 'Escape') {
      if (escapeDraw(doc, draw)) {
        overlays.clearDrawPreview();
        toolbar.setMode(MODE_SELECT);
        refresh();
        showStatus('Draw cancelled — Select mode.', false);
      }
      return;
    }

    if (ev.key === 'Enter' && isPolylineMode(doc.mode)) {
      ev.preventDefault();
      doFinishDraw();
      return;
    }

    if (ev.key === 'f' || ev.key === 'F') {
      overlays.fitBounds();
      return;
    }

    if (ev.key === 'v' || ev.key === 'V') {
      rail.setTab('validate');
      return;
    }

    if (ev.key === 'Delete' || ev.key === 'Backspace') {
      const sel = normalizeSelection(doc.selection);
      if (!sel) return;
      ev.preventDefault();
      if (sel.type === 'surface') {
        const s = doc.airport?.surfaces?.[sel.index];
        const label = s?.name || 'surface';
        if (!window.confirm(`Delete surface ${label}?`)) return;
        deleteSurface(doc, sel.index);
        afterAptMutation();
        showStatus(`Deleted ${label}.`, false);
      } else if (sel.type === 'aircraft') {
        const ac = doc.aircraft?.[sel.index];
        const cs = ac?.callsign || 'aircraft';
        if (!window.confirm(`Delete aircraft ${cs}?`)) return;
        deleteAircraft(doc, sel.index);
        afterAirMutation();
        showStatus(`Deleted ${cs}.`, false);
      }
    }
  }

  /**
   * @param {BeforeUnloadEvent} ev
   */
  function onBeforeUnload(ev) {
    if (doc.aptDirty || doc.airDirty) {
      ev.preventDefault();
      ev.returnValue = '';
    }
  }

  function doFinishDraw() {
    const result = finishDraw(doc, draw);
    overlays.clearDrawPreview();
    if (result.action === 'error') {
      showStatus(result.error || 'Cannot finish', true);
      return;
    }
    if (result.action === 'none') {
      if (result.message) showStatus(result.message, false);
      return;
    }
    toolbar.setMode(MODE_SELECT);
    afterAptMutation();
    rail.setTab('surfaces');
    showStatus(result.message || 'Surface added.', false);
  }

  function downloadDirtySides() {
    const aptD = doc.aptDirty && doc.airport;
    const airD = doc.airDirty && doc.aircraft?.length;
    if (aptD && airD) {
      if (!window.confirm('Download both dirty APT and AIR?')) return;
      downloadApt(doc);
      downloadAir(doc);
      refresh();
      showStatus('Downloading .apt and .air…', false);
      return;
    }
    if (aptD) {
      const res = downloadApt(doc);
      refresh();
      showStatus(res.ok ? `Downloading ${res.filename}…` : 'Nothing to download.', !res.ok);
      return;
    }
    if (airD) {
      const res = downloadAir(doc);
      refresh();
      showStatus(res.ok ? `Downloading ${res.filename}…` : 'Nothing to download.', !res.ok);
      return;
    }
    showStatus('Nothing dirty to download.', false);
  }

  /**
   * @param {{ fit?: boolean }} [opts]
   */
  function afterAptMutation(opts = {}) {
    validateDocument(doc);
    syncFallbackTextareas();
    refresh(opts);
  }

  /**
   * @param {{ fit?: boolean }} [opts]
   */
  function afterAirMutation(opts = {}) {
    validateDocument(doc);
    syncFallbackTextareas();
    refresh(opts);
  }

  /**
   * @param {{ fit?: boolean }} [opts]
   */
  function refresh(opts = {}) {
    overlays.render(doc.airport, doc.aircraft, doc.selection);
    if (draw.active && draw.points.length) {
      overlays.setDrawPreview(draw.points, draw.kind || SurfaceTaxiway);
    }
    rail.render(doc);
    applyTitlebarChips(root, titlebarChips(doc));
    // Mode indicator on root for CSS/cursor
    root.setAttribute('data-mode', doc.mode || MODE_SELECT);
    if (opts.fit) {
      overlays.fitBounds();
    }
  }

  function syncFallbackTextareas() {
    const aptTa = document.getElementById('apt_text');
    const airTa = document.getElementById('air_text');
    if (aptTa instanceof HTMLTextAreaElement && doc.airport) {
      aptTa.value = formatAPT(doc.airport);
    }
    if (airTa instanceof HTMLTextAreaElement) {
      airTa.value = doc.aircraft?.length ? formatAIR(doc.aircraft) : '';
    }
    const aptFn = document.getElementById('apt_filename');
    const airFn = document.getElementById('air_filename');
    if (aptFn instanceof HTMLInputElement && doc.aptFilename) {
      aptFn.value = sanitizeFilename(doc.aptFilename, 'airport.apt');
    }
    if (airFn instanceof HTMLInputElement && doc.airFilename) {
      airFn.value = sanitizeFilename(doc.airFilename, 'scenario.air');
    }
  }

  /**
   * @param {string} msg
   * @param {boolean} isError
   */
  function showStatus(msg, isError) {
    const el = root.querySelector('[data-js="status"]');
    if (!el) return;
    el.hidden = !msg;
    el.textContent = msg || '';
    el.classList.toggle('apted-flash-err', !!isError);
    el.classList.toggle('apted-flash-ok', !isError && !!msg);
    el.setAttribute('role', isError ? 'alert' : 'status');
  }

  // Silence unused kind imports for tree-shaking edge cases
  void SurfaceParking;
  void SurfaceHold;
  void SurfaceRunway;

  refresh();
}

/**
 * @param {unknown} err
 */
function errMessage(err) {
  if (err && typeof err === 'object' && 'message' in err) {
    return String(/** @type {{ message: unknown }} */ (err).message);
  }
  return String(err);
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', main);
} else {
  main();
}
