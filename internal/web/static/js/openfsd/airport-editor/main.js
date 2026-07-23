/**
 * Airport editor bootstrap (read-only map + lists).
 *
 * Progressive enhancement: requires Leaflet (global L) + this module.
 * Without JS: textareas + echo-download forms still work.
 *
 * Hard scope (PR6): open APT/AIR → parse → render map/list → select inspector
 * view → fit bounds → basemap toggle. No draw, no Blob download.
 */

import { createEmptyDocument, setAirport, setAircraft } from './model.js';
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
import { mountToolbar, readFileAsText } from './ui-toolbar.js';
import { mountRail, applyTitlebarChips } from './ui-rail.js';

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

  const blankTiles = root.getAttribute('data-test-tiles') === 'blank';
  const mapCtl = createMap(L, mapEl, { blankTiles });
  /** @type {string} */
  let activeBase = mapCtl.activeBase; // 'osm' | 'esri' | 'blank'

  const overlays = new OverlayController(L, mapCtl.map, {
    planeIconUrl: '/static/images/plane.png',
    onSelect(sel) {
      doc.selection = normalizeSelection(sel);
      refresh();
    },
  });

  const rail = mountRail(root, {
    onSelect(sel) {
      doc.selection = normalizeSelection(sel);
      refresh();
    },
  });

  const toolbar = mountToolbar(root, {
    async onOpenApt(file) {
      try {
        const text = await readFileAsText(file);
        const { airport, errors } = parseAPT(text);
        setAirport(doc, airport, {
          filename: file.name || 'airport.apt',
          errors,
          markDirty: false,
        });
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
      try {
        const text = await readFileAsText(file);
        const { aircraft, errors } = parseAIR(text);
        setAircraft(doc, aircraft, {
          filename: file.name || 'scenario.air',
          errors,
          markDirty: false,
        });
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
      if (!ok) showStatus('Nothing to fit — open a .apt or .air first.', false);
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
  });

  toolbar.setLayerLabel(blankTiles ? 'Blank' : 'OSM');

  requestAnimationFrame(() => {
    mapCtl.map.invalidateSize();
  });
  window.addEventListener('resize', () => {
    mapCtl.map.invalidateSize();
  });

  /**
   * @param {{ fit?: boolean }} [opts]
   */
  function refresh(opts = {}) {
    overlays.render(doc.airport, doc.aircraft, doc.selection);
    rail.render(doc);
    applyTitlebarChips(root, titlebarChips(doc));
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

/**
 * @param {string} name
 * @param {string} fallback
 */
function sanitizeFilename(name, fallback) {
  const base = String(name || '').split(/[/\\]/).pop() || '';
  if (/^[A-Za-z0-9._\-]{1,64}$/.test(base)) return base;
  return fallback;
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', main);
} else {
  main();
}
