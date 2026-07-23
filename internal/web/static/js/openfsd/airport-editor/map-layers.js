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
 * @returns {{ icao: string, apt: string, air: string, counts: string }}
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
  return { icao, apt, air, counts: parts.join(' · ') };
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

  // Standard layer control when not blank (OSM / Esri).
  if (!opts.blankTiles) {
    L.control
      .layers(
        {
          'OSM Standard': baseLayers.osm,
          'Esri Imagery': baseLayers.esri,
        },
        null,
        { position: 'topright', collapsed: true },
      )
      .addTo(map);
  }

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
  }

  return { map, baseLayers, get activeBase() { return activeBase; }, setBase };
}

/**
 * Overlay controller: surfaces + aircraft on a feature group.
 * Read-only: click selects; no drag/edit.
 */
export class OverlayController {
  /**
   * @param {typeof globalThis.L} L
   * @param {*} map Leaflet map
   * @param {{ planeIconUrl?: string, onSelect?: (sel: {type:string,index:number}|null) => void }} [opts]
   */
  constructor(L, map, opts = {}) {
    this.L = L;
    this.map = map;
    this.onSelect = opts.onSelect || (() => {});
    this.planeIconUrl = opts.planeIconUrl || '/static/images/plane.png';
    this.group = L.featureGroup().addTo(map);
    /** @type {Map<string, *>} */
    this._layerByKey = new Map();
    /** @type {{ type: string, index: number }|null} */
    this._selection = null;
    /** @type {import('./model.js').Airport|null} */
    this._airport = null;
    /** @type {import('./model.js').Aircraft[]} */
    this._aircraft = [];
    this._planeIcon = L.icon({
      iconUrl: this.planeIconUrl,
      iconSize: [16, 16],
      iconAnchor: [8, 8],
    });
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
    this._layerByKey.clear();

    const surfaces = airport?.surfaces || [];
    for (let i = 0; i < surfaces.length; i++) {
      this._addSurface(surfaces[i], i);
    }
    for (let i = 0; i < this._aircraft.length; i++) {
      this._addAircraft(this._aircraft[i], i);
    }
  }

  /**
   * @param {{ type: string, index: number }|null} selection
   */
  setSelection(selection) {
    this._selection = normalizeSelection(selection);
    // Re-render styles for selection highlight without full data replace.
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
      const latlngs = pts.map((p) => [p.lat, p.lon]);
      layer = L.polyline(latlngs, {
        color: style.color,
        weight: style.weight,
        opacity: style.opacity,
        dashArray: style.dashArray,
      });
      // Hold with multiple points: still polyline with dash.
      if (surface.kind === SurfaceHold && pts.length === 1) {
        // already handled above
      }
    }

    const label = surfaceLabel(surface);
    layer.bindTooltip(label, { sticky: true, direction: 'top' });
    layer.on('click', (ev) => {
      if (ev.originalEvent) L.DomEvent.stopPropagation(ev.originalEvent);
      this.onSelect({ type: 'surface', index });
    });
    layer.addTo(this.group);
    this._layerByKey.set(key, layer);
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
      title: ac.callsign || '',
      opacity: selected ? 1 : 0.92,
      zIndexOffset: selected ? 1000 : 0,
    };
    const marker = L.marker([ac.lat, ac.lon], opts);
    const tip = `${ac.callsign || '?'} · ${ac.type || ''} · hdg ${hdg}`;
    marker.bindTooltip(tip, { sticky: true, direction: 'top' });
    marker.on('click', (ev) => {
      if (ev.originalEvent) L.DomEvent.stopPropagation(ev.originalEvent);
      this.onSelect({ type: 'aircraft', index });
    });
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
