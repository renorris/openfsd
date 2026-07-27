/**
 * Leaflet map layers for the airport editor (read-only render).
 *
 * Pure helpers (bounds, styles, titlebar chips) are DOM-free for Node tests.
 * Leaflet-facing code expects global `L` when createMap / OverlayController run.
 */

import {
  SurfaceParking,
  SurfaceRunway,
  SurfaceTaxiway,
  SurfaceHold,
} from './model.js';

/** OSM Standard tiles (default). Attribution required — do not strip. */
export const OSM_TILE_URL = 'https://tile.openstreetmap.org/{z}/{x}/{y}.png';
export const OSM_ATTRIBUTION =
  '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>';

/** Esri World Imagery (optional basemap). Attribution required. */
export const ESRI_TILE_URL =
  'https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}';
export const ESRI_ATTRIBUTION =
  'Tiles &copy; Esri — Source: Esri, Maxar, Earthstar Geographics, and the GIS User Community';

/** Transparent 1×1 PNG as data URI for blank e2e tiles. */
export const BLANK_TILE_DATA_URI =
  'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';

/**
 * Style for a surface kind. Selected uses highlight border color.
 * @param {string} kind
 * @param {boolean} [selected=false]
 * @returns {{ color: string, weight: number, opacity: number, fillColor?: string, fillOpacity?: number, radius?: number, dashArray?: string }}
 */
export function surfaceStyle(kind, selected = false) {
  const sel = !!selected;
  switch (kind) {
    case SurfaceRunway:
      return {
        color: sel ? '#4a7ab0' : '#1a1a1a',
        weight: sel ? 6 : 5,
        opacity: 0.95,
      };
    case SurfaceTaxiway:
      return {
        color: sel ? '#4a7ab0' : '#5a7a9a',
        weight: sel ? 4 : 3,
        opacity: 0.9,
      };
    case SurfaceHold:
      return {
        color: sel ? '#4a7ab0' : '#c45c26',
        weight: sel ? 4 : 3,
        opacity: 0.95,
        dashArray: '6 4',
      };
    case SurfaceParking:
    default:
      return {
        color: sel ? '#4a7ab0' : '#2d6a4f',
        weight: sel ? 3 : 2,
        opacity: 1,
        fillColor: sel ? '#4a7ab0' : '#40916c',
        fillOpacity: 0.85,
        radius: sel ? 7 : 5,
      };
  }
}

/**
 * Collect all [lat, lon] pairs from airport surfaces + aircraft for bounds.
 * Pure — no Leaflet.
 * @param {import('./model.js').Airport|null|undefined} airport
 * @param {import('./model.js').Aircraft[]|null|undefined} aircraft
 * @returns {Array<[number, number]>}
 */
export function collectLatLngs(airport, aircraft) {
  /** @type {Array<[number, number]>} */
  const out = [];
  if (airport && Array.isArray(airport.surfaces)) {
    for (const s of airport.surfaces) {
      const pts = s.points || [];
      for (const p of pts) {
        if (Number.isFinite(p.lat) && Number.isFinite(p.lon)) {
          out.push([p.lat, p.lon]);
        }
      }
    }
  }
  if (Array.isArray(aircraft)) {
    for (const ac of aircraft) {
      if (Number.isFinite(ac.lat) && Number.isFinite(ac.lon)) {
        out.push([ac.lat, ac.lon]);
      }
    }
  }
  return out;
}

/**
 * Surface counts by kind for titlebar summary.
 * @param {import('./model.js').Surface[]|null|undefined} surfaces
 * @returns {{ park: number, rwy: number, taxi: number, hold: number, total: number }}
 */
export function countSurfacesByKind(surfaces) {
  const counts = { park: 0, rwy: 0, taxi: 0, hold: 0, total: 0 };
  if (!Array.isArray(surfaces)) return counts;
  for (const s of surfaces) {
    counts.total++;
    switch (s.kind) {
      case SurfaceParking:
        counts.park++;
        break;
      case SurfaceRunway:
        counts.rwy++;
        break;
      case SurfaceTaxiway:
        counts.taxi++;
        break;
      case SurfaceHold:
        counts.hold++;
        break;
      default:
        break;
    }
  }
  return counts;
}

/**
 * Titlebar chip strings from document (pure).
 * @param {import('./model.js').EditorDocument} doc
 * @returns {{ icao: string, apt: string, air: string, counts: string, issues: string, issuesTone: 'ok'|'err'|'warn'|'empty' }}
 */
export function titlebarChips(doc) {
  const icao = doc.airport?.icao ? String(doc.airport.icao).toUpperCase() : '—';
  const apt = doc.aptDirty ? 'APT*' : 'APT';
  const air = doc.airDirty ? 'AIR*' : 'AIR';
  const sc = countSurfacesByKind(doc.airport?.surfaces);
  const acN = Array.isArray(doc.aircraft) ? doc.aircraft.length : 0;
  const parts = [];
  if (sc.park) parts.push(`${sc.park} park`);
  if (sc.rwy) parts.push(`${sc.rwy} rwy`);
  if (sc.taxi) parts.push(`${sc.taxi} taxi`);
  if (sc.hold) parts.push(`${sc.hold} hold`);
  if (acN) parts.push(`${acN} ac`);

  const aptN = Array.isArray(doc.aptErrors) ? doc.aptErrors.length : 0;
  const airN = Array.isArray(doc.airErrors) ? doc.airErrors.length : 0;
  const softN = Array.isArray(doc.softWarnings) ? doc.softWarnings.length : 0;
  const total = aptN + airN + softN;
  const hasContent = !!doc.airport || acN > 0;
  /** @type {'ok'|'err'|'warn'|'empty'} */
  let issuesTone = 'empty';
  let issues = '—';
  if (!hasContent && total === 0) {
    issues = '—';
    issuesTone = 'empty';
  } else if (total === 0) {
    issues = 'OK';
    issuesTone = 'ok';
  } else if (aptN + airN > 0) {
    issues = `${total} issue${total === 1 ? '' : 's'}`;
    issuesTone = 'err';
  } else {
    issues = `${softN} warn${softN === 1 ? '' : 's'}`;
    issuesTone = 'warn';
  }

  return { icao, apt, air, counts: parts.join(' · '), issues, issuesTone };
}

/**
 * Normalize selection for comparisons.
 * @param {*} sel
 * @returns {{ type: 'surface'|'aircraft', index: number }|null}
 */
export function normalizeSelection(sel) {
  if (!sel || typeof sel !== 'object') return null;
  if (sel.type === 'surface' && Number.isInteger(sel.index) && sel.index >= 0) {
    return { type: 'surface', index: sel.index };
  }
  if (sel.type === 'aircraft' && Number.isInteger(sel.index) && sel.index >= 0) {
    return { type: 'aircraft', index: sel.index };
  }
  return null;
}

/**
 * Escape text for safe inclusion where a consumer might treat the string as HTML.
 * Pure — Node-testable. Prefer bindTextTooltip (textContent) for Leaflet.
 * @param {unknown} s
 * @returns {string}
 */
export function escapeHtml(s) {
  return String(s ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

/**
 * Visual radius (px) of vertex circleMarkers. circleMarker is always centered on
 * the lat/lng — do not use divIcon/L.marker for nodes (iconAnchor/CSS margin fights).
 */
export const VERTEX_HANDLE_RADIUS = 5;

/** @deprecated use VERTEX_HANDLE_RADIUS; kept as diameter for older tests/docs */
export const VERTEX_HANDLE_PX = VERTEX_HANDLE_RADIUS * 2;

/**
 * Pixel radius for vertex grab hit-test (capture-phase, independent of icon DOM hits).
 * Larger than the visual handle so small nodes stay easy to grab.
 */
export const VERTEX_HIT_PX = 14;

/** Duration after dragend during which map clicks are ignored. */
export const MAP_CLICK_SUPPRESS_MS = 250;

/**
 * After a feature (surface/aircraft) click, ignore map clicks for this long.
 * Leaflet re-fires the same DOM click onto the map unless originalEvent._stopped
 * is set; we do both _stopped and this time gate.
 */
export const FEATURE_CLICK_SUPPRESS_MS = 100;

/**
 * Leaflet pane name for vertex handle visuals.
 * createPane("aptedVertex") → CSS class "leaflet-aptedVertex-pane"
 * (Leaflet strips a trailing "Pane" suffix from the name when building the class).
 */
export const VERTEX_PANE = 'aptedVertex';

/**
 * Pure: Leaflet circleMarker options for a vertex node (always lat/lng-centered).
 * Drag is NOT Marker.draggable — OverlayController uses capture-phase map hit-test.
 * @param {number} vertexIndex
 * @param {{ selected?: boolean, dragging?: boolean }} [state]
 * @returns {object}
 */
export function buildVertexHandleStyle(vertexIndex, state = {}) {
  const selected = !!state.selected;
  const dragging = !!state.dragging;
  return {
    radius: VERTEX_HANDLE_RADIUS,
    // Interactive false: pointer events pass through to map capture hit-test.
    interactive: false,
    bubblingMouseEvents: false,
    weight: dragging ? 2 : 1.5,
    opacity: 1,
    fillOpacity: 1,
    color: dragging ? '#1a3a60' : selected ? '#2a5a90' : '#4a7ab0',
    fillColor: dragging ? '#d0e4f8' : selected ? '#e8f0fa' : '#ffffff',
    className:
      'apted-vertex-handle' +
      (selected ? ' is-selected' : '') +
      (dragging ? ' is-dragging' : ''),
    // title is not a path option; kept only for call-site docs
    // (vertexIndex used so the API stays stable for tests)
    _vertexIndex: vertexIndex,
  };
}

/**
 * @deprecated Prefer buildVertexHandleStyle (circleMarker). Kept for test compat.
 * @param {number} vertexIndex
 * @returns {object}
 */
export function buildVertexHandleOptions(vertexIndex) {
  return {
    draggable: false,
    interactive: false,
    keyboard: false,
    autoPan: false,
    bubblingMouseEvents: false,
    title: `Vertex ${vertexIndex + 1} — drag to move`,
  };
}

/**
 * @deprecated Prefer buildVertexHandleStyle (circleMarker). Kept for test compat.
 * @returns {{ className: string, iconSize: [number, number], iconAnchor: [number, number] }}
 */
export function buildVertexHandleIconOptions() {
  const px = VERTEX_HANDLE_PX;
  return {
    className: 'leaflet-div-icon apted-vertex-handle',
    iconSize: [px, px],
    // Center of the box — only meaningful if CSS does NOT zero Leaflet's margins.
    iconAnchor: [px / 2, px / 2],
  };
}

/**
 * Pure: nearest vertex index within maxDistPx of a click in the same pixel space.
 * Used by capture-phase map drag so map pan cannot steal the gesture.
 *
 * @param {{ x: number, y: number }[]} verticesPx  screen/container points per vertex
 * @param {{ x: number, y: number }} clickPx
 * @param {number} maxDistPx
 * @returns {{ index: number, dist: number }|null}
 */
export function findNearestVertexPx(verticesPx, clickPx, maxDistPx) {
  if (!Array.isArray(verticesPx) || !clickPx) return null;
  const max = Number(maxDistPx);
  if (!Number.isFinite(max) || max < 0) return null;
  const cx = clickPx.x;
  const cy = clickPx.y;
  if (!Number.isFinite(cx) || !Number.isFinite(cy)) return null;

  let bestIdx = -1;
  let bestDist = max;
  for (let i = 0; i < verticesPx.length; i++) {
    const v = verticesPx[i];
    if (!v || !Number.isFinite(v.x) || !Number.isFinite(v.y)) continue;
    const dx = v.x - cx;
    const dy = v.y - cy;
    const d = Math.sqrt(dx * dx + dy * dy);
    if (d <= bestDist) {
      bestDist = d;
      bestIdx = i;
    }
  }
  if (bestIdx < 0) return null;
  return { index: bestIdx, dist: bestDist };
}

/**
 * Pure: nearest vertex across many surfaces (for grab-without-select).
 *
 * @param {{ surfaceIndex: number, points: { x: number, y: number }[] }[]} surfacesPx
 * @param {{ x: number, y: number }} clickPx
 * @param {number} maxDistPx
 * @returns {{ surfaceIndex: number, vertexIndex: number, dist: number }|null}
 */
export function findNearestVertexAcrossSurfaces(surfacesPx, clickPx, maxDistPx) {
  if (!Array.isArray(surfacesPx) || !clickPx) return null;
  const max = Number(maxDistPx);
  if (!Number.isFinite(max) || max < 0) return null;

  /** @type {{ surfaceIndex: number, vertexIndex: number, dist: number }|null} */
  let best = null;
  for (const s of surfacesPx) {
    if (!s || !Array.isArray(s.points)) continue;
    const hit = findNearestVertexPx(s.points, clickPx, max);
    if (!hit) continue;
    if (!best || hit.dist < best.dist) {
      best = {
        surfaceIndex: s.surfaceIndex,
        vertexIndex: hit.index,
        dist: hit.dist,
      };
    }
  }
  return best;
}

/**
 * Pure: points → Leaflet latlng tuples (finite only).
 * @param {{lat:number,lon:number}[]} points
 * @returns {Array<[number, number]>}
 */
export function pointsToLatLngs(points) {
  const out = [];
  if (!Array.isArray(points)) return out;
  for (const p of points) {
    if (Number.isFinite(p?.lat) && Number.isFinite(p?.lon)) {
      out.push([p.lat, p.lon]);
    }
  }
  return out;
}

/**
 * Pure: apply latlngs to a surface layer duck-typed like Leaflet polyline/circleMarker.
 * Prefer setLatLngs (polyline) then setLatLng (circleMarker). No-op if neither.
 * @param {{ setLatLngs?: Function, setLatLng?: Function }|null|undefined} layer
 * @param {Array<[number, number]>} latlngs
 * @returns {'polyline'|'point'|'none'}
 */
export function applySurfaceLatLngs(layer, latlngs) {
  if (!layer || !latlngs || latlngs.length === 0) return 'none';
  if (typeof layer.setLatLngs === 'function') {
    layer.setLatLngs(latlngs);
    return 'polyline';
  }
  if (typeof layer.setLatLng === 'function') {
    layer.setLatLng(latlngs[0]);
    return 'point';
  }
  return 'none';
}

/**
 * Pure: patch one vertex in a points array (immutable-style new array).
 * @param {{lat:number,lon:number}[]} points
 * @param {number} vertexIndex
 * @param {number} lat
 * @param {number} lon
 * @returns {{lat:number,lon:number}[]}
 */
export function patchVertexPoints(points, vertexIndex, lat, lon) {
  const src = Array.isArray(points) ? points : [];
  return src.map((p, i) =>
    i === vertexIndex ? { lat, lon } : { lat: p.lat, lon: p.lon },
  );
}

/**
 * Pure: whether a map click should be ignored (active drag or post-drag suppress window).
 * Post-drag suppress does NOT block full render — only map click handlers use this.
 * @param {{ dragging: boolean, dragEndedAt: number, suppressMs: number }} state
 * @param {number} [now]
 * @returns {boolean}
 */
export function shouldSuppressMapClick(state, now = Date.now()) {
  if (state.dragging) return true;
  if (
    state.dragEndedAt > 0 &&
    now - state.dragEndedAt < state.suppressMs
  ) {
    return true;
  }
  if (
    state.featureClickAt > 0 &&
    now - state.featureClickAt < (state.featureSuppressMs ?? FEATURE_CLICK_SUPPRESS_MS)
  ) {
    return true;
  }
  return false;
}

/**
 * Stop a Leaflet layer mouse event from also firing on the map.
 *
 * Leaflet walks targets and continues while !originalEvent._stopped. Calling
 * native stopPropagation() alone does NOT set _stopped on modern browsers, so
 * map click still runs (and was clearing selection immediately after select).
 *
 * @param {*} ev Leaflet event with optional originalEvent
 * @param {typeof globalThis.L} [L]
 */
export function stopLeafletClickBubble(ev, L) {
  if (!ev) return;
  const dom = ev.originalEvent;
  if (dom) {
    // Leaflet propagation flag (required — see _fireDOMEvent in leaflet.js).
    dom._stopped = true;
    if (L && L.DomEvent) {
      L.DomEvent.stopPropagation(dom);
      L.DomEvent.preventDefault(dom);
    } else {
      if (typeof dom.stopPropagation === 'function') dom.stopPropagation();
      if (typeof dom.preventDefault === 'function') dom.preventDefault();
    }
  }
}

/**
 * Build a Leaflet tooltip content node with textContent only (never innerHTML).
 * Leaflet 1.9 uses innerHTML for string content — always pass an Element.
 * @param {string} text
 * @returns {HTMLElement}
 */
export function tooltipTextNode(text) {
  const span = document.createElement('span');
  span.textContent = String(text ?? '');
  return span;
}

/**
 * Bind a plain-text tooltip (file-derived labels must not go through innerHTML).
 * @param {*} layer Leaflet layer
 * @param {string} text
 * @param {object} [opts]
 */
export function bindTextTooltip(layer, text, opts = {}) {
  layer.bindTooltip(tooltipTextNode(text), {
    sticky: true,
    direction: 'top',
    ...opts,
  });
}

/**
 * Create blank tile layer for e2e (no network). Uses L.tileLayer with data-URI
 * or L.gridLayer empty tiles when available.
 * @param {typeof globalThis.L} L
 * @returns {*} Leaflet layer
 */
export function createBlankTileLayer(L) {
  if (typeof L.gridLayer === 'function') {
    return L.gridLayer({
      attribution: 'blank tiles (e2e)',
      tileSize: 256,
      createTile: function () {
        const tile = document.createElement('div');
        tile.style.width = '256px';
        tile.style.height = '256px';
        tile.style.background = '#dfe6ea';
        return tile;
      },
    });
  }
  // Fallback: single transparent data-URI "tile" pattern via tileLayer
  return L.tileLayer(BLANK_TILE_DATA_URI, {
    maxZoom: 19,
    attribution: 'blank tiles (e2e)',
  });
}

/**
 * Create OSM + Esri basemap layers (or blank when testTiles).
 * @param {typeof globalThis.L} L
 * @param {{ blank?: boolean }} [opts]
 * @returns {{ osm: *, esri: *, blank: *|null, defaultKey: 'osm'|'blank' }}
 */
export function createBaseLayers(L, opts = {}) {
  if (opts.blank) {
    const blank = createBlankTileLayer(L);
    return { osm: blank, esri: blank, blank, defaultKey: 'blank' };
  }
  const osm = L.tileLayer(OSM_TILE_URL, {
    maxZoom: 19,
    attribution: OSM_ATTRIBUTION,
  });
  const esri = L.tileLayer(ESRI_TILE_URL, {
    maxZoom: 19,
    attribution: ESRI_ATTRIBUTION,
  });
  return { osm, esri, blank: null, defaultKey: 'osm' };
}

/**
 * Initialize Leaflet map on #apted-map (or given element).
 * Attribution control remains visible; optional setPrefix("") only.
 * @param {typeof globalThis.L} L
 * @param {HTMLElement} mapEl
 * @param {{ blankTiles?: boolean, center?: [number, number], zoom?: number }} [opts]
 * @returns {{ map: *, baseLayers: ReturnType<typeof createBaseLayers>, activeBase: string, setBase: (key: string) => void }}
 */
export function createMap(L, mapEl, opts = {}) {
  const center = opts.center || [30, 0];
  const zoom = opts.zoom ?? 2;
  const map = L.map(mapEl, {
    // Keep default attributionControl: true
    // Box-zoom (shift-drag blue rectangle) confuses editors; pan/zoom still work.
    boxZoom: false,
  }).setView(center, zoom);

  // Cosmetic only — must not remove attribution control or empty tile credits.
  if (map.attributionControl && typeof map.attributionControl.setPrefix === 'function') {
    map.attributionControl.setPrefix('');
  }
  // Belt-and-suspenders if boxZoom was already constructed.
  if (map.boxZoom && typeof map.boxZoom.disable === 'function') {
    map.boxZoom.disable();
  }

  const baseLayers = createBaseLayers(L, { blank: !!opts.blankTiles });
  let activeBase = baseLayers.defaultKey;
  const layerByKey = {
    osm: baseLayers.osm,
    esri: baseLayers.esri,
    blank: baseLayers.blank || baseLayers.osm,
  };
  layerByKey[activeBase].addTo(map);

  // Toolbar Layer button is the sole basemap control (dense console; avoids
  // desync with a second L.control.layers widget). Attribution still updates
  // when the active tile layer changes.

  /**
   * @param {string} key
   */
  function setBase(key) {
    const next = layerByKey[key];
    if (!next) return;
    const prev = layerByKey[activeBase];
    if (prev && map.hasLayer(prev)) map.removeLayer(prev);
    if (!map.hasLayer(next)) next.addTo(map);
    activeBase = key;
    // Basemap swap after layout changes can leave a half-covered tile pane.
    if (typeof map.invalidateSize === 'function') {
      map.invalidateSize({ animate: false });
    }
  }

  // Force a remeasure after the first paint of this host (flex/grid settle).
  if (typeof map.whenReady === 'function') {
    map.whenReady(() => {
      if (typeof map.invalidateSize === 'function') {
        map.invalidateSize({ animate: false });
      }
    });
  }

  return { map, baseLayers, get activeBase() { return activeBase; }, setBase };
}

/**
 * Ensure a high z-index pane exists for vertex handle *visuals*.
 * Pane is non-interactive — grab is done via capture-phase hit-test on the map
 * container so Leaflet Map.Drag never sees the mousedown.
 * @param {*} map Leaflet map
 */
export function ensureVertexPane(map) {
  if (!map || typeof map.getPane !== 'function') return;
  if (map.getPane(VERTEX_PANE)) {
    const existing = map.getPane(VERTEX_PANE);
    if (existing && existing.style) {
      existing.style.zIndex = existing.style.zIndex || '660';
      existing.style.pointerEvents = 'none';
    }
    return;
  }
  if (typeof map.createPane === 'function') {
    const pane = map.createPane(VERTEX_PANE);
    if (pane && pane.style) {
      // Above marker pane (600); below popup (700).
      pane.style.zIndex = '660';
      // Non-interactive: do not intercept map events. Hit-test owns grab.
      pane.style.pointerEvents = 'none';
    }
  }
}

/**
 * Overlay controller: surfaces + aircraft + vertex handles + draw preview.
 * PR7: vertex drag, aircraft drag, draw-preview polyline.
 */
export class OverlayController {
  /**
   * @param {typeof globalThis.L} L
   * @param {*} map Leaflet map
   * @param {{
   *   planeIconUrl?: string,
   *   onSelect?: (sel: {type:string,index:number}|null) => void,
   *   onVertexDrag?: (surfaceIndex: number, vertexIndex: number, lat: number, lon: number) => void,
   *   onVertexDragEnd?: (surfaceIndex: number, vertexIndex: number, lat: number, lon: number) => void,
   *   onAircraftDrag?: (index: number, lat: number, lon: number) => void,
   *   onAircraftDragEnd?: (index: number, lat: number, lon: number) => void,
   *   onMapClick?: (lat: number, lon: number, originalEvent: MouseEvent|undefined) => void,
   *   onMapDblClick?: (lat: number, lon: number) => void,
   *   editable?: boolean,
   * }} [opts]
   */
  constructor(L, map, opts = {}) {
    this.L = L;
    this.map = map;
    this.onSelect = opts.onSelect || (() => {});
    this.onVertexDrag = opts.onVertexDrag || (() => {});
    this.onVertexDragEnd = opts.onVertexDragEnd || (() => {});
    this.onAircraftDrag = opts.onAircraftDrag || (() => {});
    this.onAircraftDragEnd = opts.onAircraftDragEnd || (() => {});
    this.onMapClick = opts.onMapClick || null;
    this.onMapDblClick = opts.onMapDblClick || null;
    this.editable = opts.editable !== false;
    /** When false (e.g. draw modes), hide nodes and ignore vertex grab. */
    this.vertexEditActive = opts.vertexEditActive !== false;
    this.planeIconUrl = opts.planeIconUrl || '/static/images/plane.png';
    this.group = L.featureGroup().addTo(map);
    this.vertexGroup = L.featureGroup().addTo(map);
    this.previewGroup = L.featureGroup().addTo(map);
    /** @type {Map<string, *>} */
    this._layerByKey = new Map();
    /** @type {{ type: string, index: number }|null} */
    this._selection = null;
    /** @type {import('./model.js').Airport|null} */
    this._airport = null;
    /** @type {import('./model.js').Aircraft[]} */
    this._aircraft = [];
    /** @type {boolean} true only while a vertex/aircraft pointer drag is active */
    this._dragging = false;
    /** @type {number} ms timestamp of last dragend (0 = never / reset on dragstart) */
    this._dragEndedAt = 0;
    /** @type {number} ms timestamp of last feature click (blocks map deselect) */
    this._featureClickAt = 0;
    /**
     * Active manual vertex drag.
     * @type {{ surfaceIndex: number, vertexIndex: number, handle: *|null, mapDraggingWasEnabled: boolean }|null}
     */
    this._vertexDrag = null;
    /** @type {Map<string, *>} surfaceIndex:vertexIndex → handle marker */
    this._handleByKey = new Map();
    /** @type {(ev: MouseEvent|TouchEvent) => void}|null */
    this._onVertexPointerMove = null;
    /** @type {(ev: MouseEvent|TouchEvent) => void}|null */
    this._onVertexPointerUp = null;
    /** Bound capture-phase handler (stable ref for removeEventListener). */
    this._onPointerDownCapture = (ev) => this._handlePointerDownCapture(ev);
    this._planeIcon = L.icon({
      iconUrl: this.planeIconUrl,
      iconSize: [16, 16],
      iconAnchor: [8, 8],
    });

    // Visual-only pane for handles (non-interactive).
    ensureVertexPane(map);

    // RCA fix: Map.Drag listens for mousedown on map._container (bubble).
    // Capture-phase hit-test intercepts first and stopImmediatePropagation so
    // the map never starts a pan when the pointer is near a vertex.
    const container = typeof map.getContainer === 'function' ? map.getContainer() : null;
    if (container) {
      container.addEventListener('mousedown', this._onPointerDownCapture, true);
      container.addEventListener('touchstart', this._onPointerDownCapture, {
        capture: true,
        passive: false,
      });
    }

    if (this.onMapClick) {
      map.on('click', (ev) => {
        if (
          shouldSuppressMapClick(
            {
              dragging: this._dragging,
              dragEndedAt: this._dragEndedAt,
              suppressMs: MAP_CLICK_SUPPRESS_MS,
              featureClickAt: this._featureClickAt,
              featureSuppressMs: FEATURE_CLICK_SUPPRESS_MS,
            },
            Date.now(),
          )
        ) {
          return;
        }
        const ll = ev.latlng;
        this.onMapClick(ll.lat, ll.lng, ev.originalEvent);
      });
    }
    if (this.onMapDblClick) {
      map.on('dblclick', (ev) => {
        const ll = ev.latlng;
        this.onMapDblClick(ll.lat, ll.lng);
      });
    }
  }

  /**
   * @param {import('./model.js').Airport|null} airport
   * @param {import('./model.js').Aircraft[]} aircraft
   * @param {{ type: string, index: number }|null} [selection]
   */
  render(airport, aircraft, selection = null) {
    // Never rebuild mid-vertex-drag (would drop the active handle mid-gesture).
    if (this._vertexDrag) {
      this._airport = airport;
      this._aircraft = Array.isArray(aircraft) ? aircraft : [];
      // Keep selection in sync for rail without recreating layers.
      this._selection = normalizeSelection(selection);
      return;
    }

    this._airport = airport;
    this._aircraft = Array.isArray(aircraft) ? aircraft : [];
    this._selection = normalizeSelection(selection);
    this.group.clearLayers();
    this.vertexGroup.clearLayers();
    this._layerByKey.clear();
    this._handleByKey.clear();

    const surfaces = airport?.surfaces || [];
    for (let i = 0; i < surfaces.length; i++) {
      this._addSurface(surfaces[i], i);
    }
    for (let i = 0; i < this._aircraft.length; i++) {
      this._addAircraft(this._aircraft[i], i);
    }

    // Small vertex nodes on every surface — grab via capture hit-test (no pre-select).
    if (this.editable && this.vertexEditActive) {
      for (let i = 0; i < surfaces.length; i++) {
        this._addVertexHandles(surfaces[i], i);
      }
    }
  }

  /**
   * Enable/disable always-on vertex nodes + grab (typically Select mode only).
   * @param {boolean} active
   */
  setVertexEditActive(active) {
    this.vertexEditActive = !!active;
  }

  /**
   * Draw preview polyline for in-progress draw session.
   * @param {{ lat: number, lon: number }[]} points
   * @param {string} [kind]
   */
  setDrawPreview(points, kind = SurfaceTaxiway) {
    this.previewGroup.clearLayers();
    if (!points || points.length === 0) return;
    const L = this.L;
    const style = surfaceStyle(kind, true);
    if (points.length === 1) {
      const p = points[0];
      L.circleMarker([p.lat, p.lon], {
        radius: 5,
        color: style.color,
        weight: 2,
        fillColor: style.color,
        fillOpacity: 0.5,
      }).addTo(this.previewGroup);
      return;
    }
    const latlngs = points.map((p) => [p.lat, p.lon]);
    L.polyline(latlngs, {
      color: style.color,
      weight: style.weight,
      opacity: 0.7,
      dashArray: '4 6',
    }).addTo(this.previewGroup);
    for (const p of points) {
      L.circleMarker([p.lat, p.lon], {
        radius: 4,
        color: style.color,
        weight: 1,
        fillColor: '#fff',
        fillOpacity: 0.9,
      }).addTo(this.previewGroup);
    }
  }

  clearDrawPreview() {
    this.previewGroup.clearLayers();
  }

  /**
   * @param {{ type: string, index: number }|null} selection
   */
  setSelection(selection) {
    this._selection = normalizeSelection(selection);
    this.render(this._airport, this._aircraft, this._selection);
  }

  /**
   * Fit map to all geometry. No-op when empty.
   * @param {{ padding?: [number, number], maxZoom?: number }} [opts]
   * @returns {boolean} true if bounds applied
   */
  fitBounds(opts = {}) {
    const latlngs = collectLatLngs(this._airport, this._aircraft);
    if (latlngs.length === 0) return false;
    const bounds = this.L.latLngBounds(latlngs);
    if (!bounds.isValid()) return false;
    this.map.fitBounds(bounds, {
      padding: opts.padding || [28, 28],
      maxZoom: opts.maxZoom ?? 17,
    });
    return true;
  }

  /**
   * @param {import('./model.js').Surface} surface
   * @param {number} index
   */
  _addSurface(surface, index) {
    const L = this.L;
    const selected =
      this._selection?.type === 'surface' && this._selection.index === index;
    const style = surfaceStyle(surface.kind, selected);
    const pts = (surface.points || []).filter(
      (p) => Number.isFinite(p.lat) && Number.isFinite(p.lon),
    );
    if (pts.length === 0) return;

    const key = `surface:${index}`;
    let layer;

    // bubblingMouseEvents:false — do not re-fire click on the map (would clear selection).
    const pathOpts = {
      bubblingMouseEvents: false,
    };

    if (surface.kind === SurfaceParking || (surface.kind === SurfaceHold && pts.length === 1)) {
      const p = pts[0];
      layer = L.circleMarker([p.lat, p.lon], {
        radius: style.radius ?? 5,
        color: style.color,
        weight: style.weight,
        opacity: style.opacity,
        fillColor: style.fillColor || style.color,
        fillOpacity: style.fillOpacity ?? 0.85,
        ...pathOpts,
      });
    } else {
      // Multi-point surfaces (runway/taxi/hold polyline). Hold dash is in style.
      const latlngs = pts.map((p) => [p.lat, p.lon]);
      layer = L.polyline(latlngs, {
        color: style.color,
        weight: style.weight,
        opacity: style.opacity,
        dashArray: style.dashArray,
        ...pathOpts,
      });
    }

    bindTextTooltip(layer, surfaceLabel(surface));
    layer.on('click', (ev) => {
      stopLeafletClickBubble(ev, L);
      this._featureClickAt = Date.now();
      this.onSelect({ type: 'surface', index });
    });
    layer.addTo(this.group);
    this._layerByKey.set(key, layer);
  }

  /**
   * Live-update the surface layer for surfaceIndex from points (no full render).
   * Layer under surface:N is polyline (setLatLngs) or circleMarker (setLatLng).
   * @param {number} surfaceIndex
   * @param {{lat:number,lon:number}[]} points
   */
  _liveSetSurfacePoints(surfaceIndex, points) {
    const layer = this._layerByKey.get(`surface:${surfaceIndex}`);
    applySurfaceLatLngs(layer, pointsToLatLngs(points));
  }

  /**
   * Apply a vertex lat/lon during drag: move handle, reshape polyline/point live, notify model.
   * Does not call onVertexDragEnd (caller commits end after clearing _dragging).
   * @param {number} surfaceIndex
   * @param {number} vertexIndex
   * @param {number} lat
   * @param {number} lon
   * @param {*} [handle] Leaflet marker to reposition
   */
  _applyVertexDrag(surfaceIndex, vertexIndex, lat, lon, handle) {
    if (handle && typeof handle.setLatLng === 'function') {
      handle.setLatLng([lat, lon]);
    }
    const base = this._airport?.surfaces?.[surfaceIndex]?.points || [];
    const next = patchVertexPoints(base, vertexIndex, lat, lon);
    // Live-update the connecting polyline / parking point immediately.
    this._liveSetSurfacePoints(surfaceIndex, next);
    this.onVertexDrag(surfaceIndex, vertexIndex, lat, lon);
  }

  /**
   * @param {MouseEvent|TouchEvent} domEv
   * @returns {MouseEvent|Touch|null}
   */
  _pointerClient(domEv) {
    if (!domEv) return null;
    if ('touches' in domEv && domEv.touches && domEv.touches.length) {
      return domEv.touches[0];
    }
    if ('changedTouches' in domEv && domEv.changedTouches && domEv.changedTouches.length) {
      return domEv.changedTouches[0];
    }
    if ('clientX' in domEv) return /** @type {MouseEvent} */ (domEv);
    return null;
  }

  /**
   * Convert a browser pointer event to map latlng (works for mouse + touch).
   * @param {MouseEvent|TouchEvent} domEv
   * @returns {{ lat: number, lng: number }|null}
   */
  _latLngFromPointerEvent(domEv) {
    const map = this.map;
    const pt = this._pointerClient(domEv);
    if (!map || !pt || !Number.isFinite(pt.clientX) || !Number.isFinite(pt.clientY)) return null;
    try {
      return map.mouseEventToLatLng(/** @type {MouseEvent} */ (pt));
    } catch {
      return null;
    }
  }

  /**
   * Convert pointer event to map container pixel point.
   * @param {MouseEvent|TouchEvent} domEv
   * @returns {{ x: number, y: number }|null}
   */
  _containerPointFromPointerEvent(domEv) {
    const map = this.map;
    const pt = this._pointerClient(domEv);
    if (!map || !pt || !Number.isFinite(pt.clientX) || !Number.isFinite(pt.clientY)) return null;
    try {
      const p = map.mouseEventToContainerPoint(/** @type {MouseEvent} */ (pt));
      return { x: p.x, y: p.y };
    } catch {
      return null;
    }
  }

  /**
   * Hit-test every surface vertex in container pixels (no pre-select required).
   * @param {MouseEvent|TouchEvent} domEv
   * @returns {{ surfaceIndex: number, vertexIndex: number, handle: *|null }|null}
   */
  _hitTestAnyVertex(domEv) {
    if (!this.editable || !this.vertexEditActive) return null;
    const airport = this._airport;
    if (!airport || !Array.isArray(airport.surfaces) || airport.surfaces.length === 0) {
      return null;
    }

    const clickPx = this._containerPointFromPointerEvent(domEv);
    if (!clickPx) return null;

    const map = this.map;
    /** @type {{ surfaceIndex: number, points: { x: number, y: number }[] }[]} */
    const surfacesPx = [];
    for (let si = 0; si < airport.surfaces.length; si++) {
      const surface = airport.surfaces[si];
      const pts = surface?.points;
      if (!Array.isArray(pts) || pts.length === 0) continue;
      /** @type {{ x: number, y: number }[]} */
      const verticesPx = [];
      for (const p of pts) {
        if (!Number.isFinite(p?.lat) || !Number.isFinite(p?.lon)) {
          verticesPx.push({ x: NaN, y: NaN });
          continue;
        }
        try {
          const cp = map.latLngToContainerPoint([p.lat, p.lon]);
          verticesPx.push({ x: cp.x, y: cp.y });
        } catch {
          verticesPx.push({ x: NaN, y: NaN });
        }
      }
      surfacesPx.push({ surfaceIndex: si, points: verticesPx });
    }

    const hit = findNearestVertexAcrossSurfaces(surfacesPx, clickPx, VERTEX_HIT_PX);
    if (!hit) return null;
    const key = `${hit.surfaceIndex}:${hit.vertexIndex}`;
    return {
      surfaceIndex: hit.surfaceIndex,
      vertexIndex: hit.vertexIndex,
      handle: this._handleByKey.get(key) || null,
    };
  }

  /**
   * Capture-phase mousedown/touchstart on map container.
   * If near any surface vertex: stop map pan and start vertex drag (auto-selects surface).
   * @param {MouseEvent|TouchEvent} ev
   */
  _handlePointerDownCapture(ev) {
    if (!this.editable || !this.vertexEditActive || this._vertexDrag || this._dragging) {
      return;
    }
    // Ignore non-primary mouse buttons.
    if ('button' in ev && ev.button !== 0 && ev.type === 'mousedown') return;

    const hit = this._hitTestAnyVertex(ev);
    if (!hit) return;

    // Critical: prevent Leaflet Map.Drag (listens on container, bubble phase)
    // from ever seeing this pointer-down.
    if (typeof ev.preventDefault === 'function') ev.preventDefault();
    if (typeof ev.stopImmediatePropagation === 'function') ev.stopImmediatePropagation();
    else if (typeof ev.stopPropagation === 'function') ev.stopPropagation();

    // Begin drag first so onSelect → refresh sees _vertexDrag and skips rebuild.
    this._beginVertexDrag(hit.surfaceIndex, hit.vertexIndex, hit.handle, ev);

    // Select the surface so rail/highlight follow (no prior click required).
    this._featureClickAt = Date.now();
    this.onSelect({ type: 'surface', index: hit.surfaceIndex });
  }

  /**
   * Start document-level vertex drag after capture-phase hit.
   * @param {number} surfaceIndex
   * @param {number} vertexIndex
   * @param {*|null} handle
   * @param {Event} [domEv]
   */
  _beginVertexDrag(surfaceIndex, vertexIndex, handle, domEv) {
    if (!this.editable || this._vertexDrag) return;
    const map = this.map;

    // Disable map pan for the duration of the gesture.
    const mapDraggingWasEnabled =
      !!(map.dragging && typeof map.dragging.enabled === 'function' && map.dragging.enabled());
    if (map.dragging && typeof map.dragging.disable === 'function') {
      map.dragging.disable();
    }
    // If a drag already began somehow, finish it.
    try {
      const draggable = map.dragging && map.dragging._draggable;
      if (draggable && draggable._moving && typeof draggable.finishDrag === 'function') {
        draggable.finishDrag();
      }
    } catch {
      /* ignore */
    }

    this._dragging = true;
    this._dragEndedAt = 0;
    this._vertexDrag = {
      surfaceIndex,
      vertexIndex,
      handle: handle || null,
      mapDraggingWasEnabled,
    };

    // Visual feedback on the centered circleMarker.
    if (handle && typeof handle.setStyle === 'function') {
      handle.setStyle(buildVertexHandleStyle(vertexIndex, { selected: true, dragging: true }));
    }

    // Do not snap vertex to cursor on mousedown — only move once the pointer moves.
    void domEv;

    const onMove = (ev) => {
      if (!this._vertexDrag) return;
      if (ev && typeof ev.preventDefault === 'function') ev.preventDefault();
      const ll = this._latLngFromPointerEvent(ev);
      if (!ll) return;
      const { surfaceIndex: si, vertexIndex: vi, handle: h } = this._vertexDrag;
      this._applyVertexDrag(si, vi, ll.lat, ll.lng, h);
    };

    const onUp = (ev) => {
      if (!this._vertexDrag) return;
      const { surfaceIndex: si, vertexIndex: vi, handle: h, mapDraggingWasEnabled: was } =
        this._vertexDrag;

      if (this._onVertexPointerMove) {
        document.removeEventListener('mousemove', this._onVertexPointerMove);
        document.removeEventListener('touchmove', this._onVertexPointerMove);
      }
      if (this._onVertexPointerUp) {
        document.removeEventListener('mouseup', this._onVertexPointerUp);
        document.removeEventListener('touchend', this._onVertexPointerUp);
        document.removeEventListener('touchcancel', this._onVertexPointerUp);
      }
      this._onVertexPointerMove = null;
      this._onVertexPointerUp = null;
      this._vertexDrag = null;

      if (h && typeof h.setStyle === 'function') {
        const stillSelected =
          this._selection?.type === 'surface' && this._selection.index === si;
        h.setStyle(buildVertexHandleStyle(vi, { selected: stillSelected, dragging: false }));
      }

      const ll = this._latLngFromPointerEvent(ev) || (h && h.getLatLng && h.getLatLng());
      const lat = ll?.lat;
      const lon = ll?.lng ?? ll?.lon;

      // Clear pointer-down BEFORE app refresh (onVertexDragEnd → full render).
      this._dragEndedAt = Date.now();
      this._dragging = false;
      if (was && map.dragging && typeof map.dragging.enable === 'function') {
        map.dragging.enable();
      }

      if (Number.isFinite(lat) && Number.isFinite(lon)) {
        this._applyVertexDrag(si, vi, lat, lon, h);
        this.onVertexDragEnd(si, vi, lat, lon);
      }
    };

    this._onVertexPointerMove = onMove;
    this._onVertexPointerUp = onUp;
    document.addEventListener('mousemove', onMove);
    document.addEventListener('mouseup', onUp);
    document.addEventListener('touchmove', onMove, { passive: false });
    document.addEventListener('touchend', onUp);
    document.addEventListener('touchcancel', onUp);
  }

  /**
   * Small vertex handle *visuals* for a surface.
   * Uses L.circleMarker so the disc is always centered on the lat/lng
   * (divIcon + CSS margin overrides previously shifted the knob off-center).
   * Grab is owned by capture-phase hit-test on the map container (any surface).
   * @param {import('./model.js').Surface} surface
   * @param {number} surfaceIndex
   */
  _addVertexHandles(surface, surfaceIndex) {
    const L = this.L;
    const pts = surface.points || [];
    const selected =
      this._selection?.type === 'surface' && this._selection.index === surfaceIndex;

    for (let vi = 0; vi < pts.length; vi++) {
      const p = pts[vi];
      if (!Number.isFinite(p.lat) || !Number.isFinite(p.lon)) continue;
      // circleMarker: geographic center === visual center (no iconAnchor).
      const handle = L.circleMarker(
        [p.lat, p.lon],
        buildVertexHandleStyle(vi, { selected, dragging: false }),
      );
      handle.addTo(this.vertexGroup);
      this._handleByKey.set(`${surfaceIndex}:${vi}`, handle);
    }
  }

  /**
   * @param {import('./model.js').Aircraft} ac
   * @param {number} index
   */
  _addAircraft(ac, index) {
    if (!Number.isFinite(ac.lat) || !Number.isFinite(ac.lon)) return;
    const L = this.L;
    const selected =
      this._selection?.type === 'aircraft' && this._selection.index === index;
    const hdg = Number(ac.heading) || 0;
    const opts = {
      icon: this._planeIcon,
      rotationAngle: hdg,
      rotationOrigin: 'center center',
      // title is a plain attribute (not HTML); still use plain text only.
      title: String(ac.callsign || ''),
      opacity: selected ? 1 : 0.92,
      zIndexOffset: selected ? 1000 : 0,
      draggable: this.editable,
    };
    const marker = L.marker([ac.lat, ac.lon], opts);
    const tip = `${ac.callsign || '?'} · ${ac.type || ''} · hdg ${hdg}`;
    bindTextTooltip(marker, tip);
    marker.on('click', (ev) => {
      stopLeafletClickBubble(ev, L);
      this._featureClickAt = Date.now();
      this.onSelect({ type: 'aircraft', index });
    });
    if (this.editable) {
      marker.on('dragstart', (ev) => {
        this._dragging = true;
        this._dragEndedAt = 0;
        if (ev?.originalEvent) L.DomEvent.stopPropagation(ev.originalEvent);
      });
      marker.on('drag', (ev) => {
        const ll = ev.target.getLatLng();
        this.onAircraftDrag(index, ll.lat, ll.lng);
      });
      marker.on('dragend', (ev) => {
        const ll = ev.target.getLatLng();
        // Same state machine as vertex: clear _dragging before callback so full refresh runs.
        this._dragEndedAt = Date.now();
        this._dragging = false;
        this.onAircraftDragEnd(index, ll.lat, ll.lng);
      });
    }
    marker.addTo(this.group);
    this._layerByKey.set(`aircraft:${index}`, marker);

    // Subtle selection ring under plane.
    if (selected) {
      const ring = L.circleMarker([ac.lat, ac.lon], {
        radius: 12,
        color: '#4a7ab0',
        weight: 2,
        fill: false,
        opacity: 0.9,
      });
      ring.addTo(this.group);
    }
  }
}

/**
 * @param {import('./model.js').Surface} s
 * @returns {string}
 */
export function surfaceLabel(s) {
  if (!s) return '';
  if (s.kind === SurfaceRunway) {
    return `RWY ${s.name || `${s.rwyA || '?'}/${s.rwyB || '?'}`}`;
  }
  const kind = (s.kind || '').toLowerCase();
  return `${s.name || '?'} (${kind})`;
}

/**
 * Short kind label for lists.
 * @param {string} kind
 */
export function kindShort(kind) {
  switch (kind) {
    case SurfaceParking:
      return 'park';
    case SurfaceRunway:
      return 'rwy';
    case SurfaceTaxiway:
      return 'taxi';
    case SurfaceHold:
      return 'hold';
    default:
      return String(kind || '?').toLowerCase();
  }
}
