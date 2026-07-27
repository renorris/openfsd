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

/** Keep in sync with .apted-vertex-handle width/height in airport-editor.css */
export const VERTEX_HANDLE_PX = 16;

/** Duration after dragend during which map clicks are ignored. */
export const MAP_CLICK_SUPPRESS_MS = 250;

/**
 * Pure: options for the vertex L.marker (no Leaflet instance required).
 * Icon is constructed at the call site with L.divIcon({...buildVertexHandleIconOptions()}).
 * @param {number} vertexIndex
 * @returns {object}
 */
export function buildVertexHandleOptions(vertexIndex) {
  return {
    draggable: true,
    autoPan: false,
    keyboard: false,
    zIndexOffset: 2000,
    bubblingMouseEvents: false,
    title: `Vertex ${vertexIndex + 1}`,
  };
}

/**
 * Pure icon size/anchor for divIcon — symmetry asserted in unit tests.
 * @returns {{ className: string, iconSize: [number, number], iconAnchor: [number, number] }}
 */
export function buildVertexHandleIconOptions() {
  const px = VERTEX_HANDLE_PX;
  return {
    className: 'apted-vertex-handle leaflet-interactive',
    iconSize: [px, px],
    iconAnchor: [px / 2, px / 2],
  };
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
  return false;
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
  }).setView(center, zoom);

  // Cosmetic only — must not remove attribution control or empty tile credits.
  if (map.attributionControl && typeof map.attributionControl.setPrefix === 'function') {
    map.attributionControl.setPrefix('');
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
    this._planeIcon = L.icon({
      iconUrl: this.planeIconUrl,
      iconSize: [16, 16],
      iconAnchor: [8, 8],
    });

    if (this.onMapClick) {
      map.on('click', (ev) => {
        if (
          shouldSuppressMapClick(
            {
              dragging: this._dragging,
              dragEndedAt: this._dragEndedAt,
              suppressMs: MAP_CLICK_SUPPRESS_MS,
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
    this._airport = airport;
    this._aircraft = Array.isArray(aircraft) ? aircraft : [];
    this._selection = normalizeSelection(selection);
    this.group.clearLayers();
    this.vertexGroup.clearLayers();
    this._layerByKey.clear();

    const surfaces = airport?.surfaces || [];
    for (let i = 0; i < surfaces.length; i++) {
      this._addSurface(surfaces[i], i);
    }
    for (let i = 0; i < this._aircraft.length; i++) {
      this._addAircraft(this._aircraft[i], i);
    }

    // Vertex handles for selected surface (edit mode).
    if (this.editable && this._selection?.type === 'surface') {
      const s = surfaces[this._selection.index];
      if (s) this._addVertexHandles(s, this._selection.index);
    }
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

    if (surface.kind === SurfaceParking || (surface.kind === SurfaceHold && pts.length === 1)) {
      const p = pts[0];
      layer = L.circleMarker([p.lat, p.lon], {
        radius: style.radius ?? 5,
        color: style.color,
        weight: style.weight,
        opacity: style.opacity,
        fillColor: style.fillColor || style.color,
        fillOpacity: style.fillOpacity ?? 0.85,
      });
    } else {
      // Multi-point surfaces (runway/taxi/hold polyline). Hold dash is in style.
      const latlngs = pts.map((p) => [p.lat, p.lon]);
      layer = L.polyline(latlngs, {
        color: style.color,
        weight: style.weight,
        opacity: style.opacity,
        dashArray: style.dashArray,
      });
    }

    bindTextTooltip(layer, surfaceLabel(surface));
    layer.on('click', (ev) => {
      if (ev.originalEvent) L.DomEvent.stopPropagation(ev.originalEvent);
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
   * Draggable vertex handles for selected surface.
   * Single divIcon marker per vertex (no companion circleMarker).
   * Normative drag state machine: paint first, clear _dragging before onVertexDragEnd.
   * @param {import('./model.js').Surface} surface
   * @param {number} surfaceIndex
   */
  _addVertexHandles(surface, surfaceIndex) {
    const L = this.L;
    const pts = surface.points || [];
    for (let vi = 0; vi < pts.length; vi++) {
      const p = pts[vi];
      if (!Number.isFinite(p.lat) || !Number.isFinite(p.lon)) continue;
      const handle = L.marker([p.lat, p.lon], {
        ...buildVertexHandleOptions(vi),
        icon: L.divIcon(buildVertexHandleIconOptions()),
      });
      handle.on('dragstart', (ev) => {
        this._dragging = true;
        this._dragEndedAt = 0;
        if (ev?.originalEvent) L.DomEvent.stopPropagation(ev.originalEvent);
      });
      handle.on('drag', (ev) => {
        const ll = ev.target.getLatLng();
        // 1) Overlay-local paint FIRST (order-independent of model callback).
        const base = this._airport?.surfaces?.[surfaceIndex]?.points || [];
        const next = patchVertexPoints(base, vi, ll.lat, ll.lng);
        this._liveSetSurfacePoints(surfaceIndex, next);
        // 2) Model commit path (main must not refresh).
        this.onVertexDrag(surfaceIndex, vi, ll.lat, ll.lng);
      });
      handle.on('dragend', (ev) => {
        const ll = ev.target.getLatLng();
        // Final live paint (covers last frame).
        const base = this._airport?.surfaces?.[surfaceIndex]?.points || [];
        const next = patchVertexPoints(base, vi, ll.lat, ll.lng);
        this._liveSetSurfacePoints(surfaceIndex, next);
        // State machine: clear pointer-down flag BEFORE app refresh path.
        this._dragEndedAt = Date.now();
        this._dragging = false;
        this.onVertexDragEnd(surfaceIndex, vi, ll.lat, ll.lng);
      });
      handle.addTo(this.vertexGroup);
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
      if (ev.originalEvent) L.DomEvent.stopPropagation(ev.originalEvent);
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
