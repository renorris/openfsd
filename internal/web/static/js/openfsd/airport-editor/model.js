/**
 * Pure document model helpers for the airport editor.
 * Mirrors pkg/twrfiles types (camelCase field names).
 * No DOM / window access — Node-testable.
 *
 * PR7: surface/aircraft mutations, dirty hash fields, mode, defaults.
 */

/** @typedef {{ lat: number, lon: number }} Point */

/**
 * @typedef {Object} Surface
 * @property {string} kind - PARKING | RUNWAY | TAXIWAY | HOLD
 * @property {string} name
 * @property {Point[]} points
 * @property {string} [rwyA]
 * @property {string} [rwyB]
 * @property {number} [dispA]
 * @property {number} [dispB]
 * @property {boolean} [turnoffLeft]
 * @property {string} [id] - client-only stable id (s1, s2, …)
 */

/**
 * @typedef {Object} Airport
 * @property {string} icao
 * @property {number} magVar
 * @property {number} fieldElev
 * @property {number} patternElev
 * @property {number} patternSize
 * @property {number} initClimbProps
 * @property {number} initClimbJets
 * @property {string} jetAirlines
 * @property {string} turboAirlines
 * @property {string} registration
 * @property {Surface[]} surfaces
 */

/**
 * @typedef {Object} Aircraft
 * @property {string} callsign
 * @property {string} type
 * @property {string} engine
 * @property {string} rules
 * @property {string} dep
 * @property {string} arr
 * @property {number} cruiseAlt
 * @property {string} route
 * @property {string} remarks
 * @property {string} squawk
 * @property {string} xpdrMode
 * @property {number} lat
 * @property {number} lon
 * @property {number} alt
 * @property {number} speed
 * @property {number} heading
 */

/**
 * @typedef {Object} EditorDocument
 * @property {Airport|null} airport
 * @property {Aircraft[]} aircraft
 * @property {boolean} aptDirty
 * @property {boolean} airDirty
 * @property {string|null} aptFilename
 * @property {string|null} airFilename
 * @property {string[]} aptErrors
 * @property {string[]} airErrors
 * @property {string[]} softWarnings
 * @property {*} selection
 * @property {string} mode - select | park | taxi | runway | hold | aircraft
 * @property {string|null} lastAptDownloadHash
 * @property {string|null} lastAirDownloadHash
 * @property {number} _nextSurfaceId
 * @property {ServerValidationState|null} [serverValidation] - last server confirm result (UI)
 */

/**
 * @typedef {Object} ServerValidationSide
 * @property {boolean} ok
 * @property {string|null} error
 * @property {string[]} errors
 * @property {{ icao?: string, surface_count?: number, aircraft_count?: number }} summary
 */

/**
 * @typedef {Object} ServerValidationState
 * @property {boolean} loading
 * @property {boolean} stale - true after local edits since last confirm
 * @property {ServerValidationSide|null} [apt]
 * @property {ServerValidationSide|null} [air]
 * @property {string} [message] - top-level status (e.g. nothing to send)
 */

// Surface kind identifiers (TWRTrainer section types).
export const SurfaceParking = 'PARKING';
export const SurfaceRunway = 'RUNWAY';
export const SurfaceTaxiway = 'TAXIWAY';
export const SurfaceHold = 'HOLD';

// Engine type codes used in .air files.
export const EnginePiston = 'P';
export const EngineTurboprop = 'T';
export const EngineJet = 'J';
export const EngineHelicopter = 'H';

// Flight-plan / rules codes.
export const RulesVFR = 'V';
export const RulesIFR = 'I';
export const RulesDVFR = 'D';
export const RulesSVFR = 'S';

// Transponder mode codes.
export const XPDRModeNormal = 'N';
export const XPDRModeStandby = 'S';

/** TWRTrainer fallback for registration= when the header omits it. */
export const DefaultRegistration = 'N';

export const DEFAULT_PATTERN_SIZE = 1.0;
export const DEFAULT_INIT_CLIMB_PROPS = 3000.0;
export const DEFAULT_INIT_CLIMB_JETS = 5000.0;

/** Editor modes (toolbar + keys 1–6). */
export const MODE_SELECT = 'select';
export const MODE_PARK = 'park';
export const MODE_TAXI = 'taxi';
export const MODE_RUNWAY = 'runway';
export const MODE_HOLD = 'hold';
export const MODE_AIRCRAFT = 'aircraft';

export const EDITOR_MODES = [
  MODE_SELECT,
  MODE_PARK,
  MODE_TAXI,
  MODE_RUNWAY,
  MODE_HOLD,
  MODE_AIRCRAFT,
];

/**
 * Minimum points per surface kind (delete-vertex guard).
 * @param {string} kind
 * @returns {number}
 */
export function minPointsForKind(kind) {
  switch (kind) {
    case SurfaceParking:
    case SurfaceHold:
      return 1;
    case SurfaceTaxiway:
    case SurfaceRunway:
      return 2;
    default:
      return 1;
  }
}

/**
 * Create an empty Airport with parse defaults.
 * @returns {Airport}
 */
export function createEmptyAirport() {
  return {
    icao: '',
    magVar: 0,
    fieldElev: 0,
    patternElev: 0,
    patternSize: DEFAULT_PATTERN_SIZE,
    initClimbProps: DEFAULT_INIT_CLIMB_PROPS,
    initClimbJets: DEFAULT_INIT_CLIMB_JETS,
    jetAirlines: '',
    turboAirlines: '',
    registration: DefaultRegistration,
    surfaces: [],
  };
}

/**
 * Create an empty editor document.
 * @returns {EditorDocument}
 */
export function createEmptyDocument() {
  return {
    airport: null,
    aircraft: [],
    aptDirty: false,
    airDirty: false,
    aptFilename: null,
    airFilename: null,
    aptErrors: [],
    airErrors: [],
    softWarnings: [],
    selection: null,
    mode: MODE_SELECT,
    lastAptDownloadHash: null,
    lastAirDownloadHash: null,
    _nextSurfaceId: 1,
    serverValidation: null,
  };
}

/**
 * Mark last server confirm result as stale after local edits (if any).
 * @param {EditorDocument} doc
 */
export function markServerValidationStale(doc) {
  if (doc?.serverValidation && !doc.serverValidation.loading) {
    doc.serverValidation = { ...doc.serverValidation, stale: true };
  }
}

/**
 * Stable string hash for dirty tracking (FNV-1a 32-bit hex).
 * Pure — Node-testable.
 * @param {string} text
 * @returns {string}
 */
export function hashText(text) {
  const s = String(text ?? '');
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  // Unsigned 32-bit hex
  return (h >>> 0).toString(16).padStart(8, '0');
}

/**
 * Mark APT side dirty.
 * @param {EditorDocument} doc
 * @returns {EditorDocument}
 */
export function markAptDirty(doc) {
  doc.aptDirty = true;
  return doc;
}

/**
 * Mark AIR side dirty.
 * @param {EditorDocument} doc
 * @returns {EditorDocument}
 */
export function markAirDirty(doc) {
  doc.airDirty = true;
  return doc;
}

/**
 * Mark one or both sides clean (user-initiated "Mark clean").
 * Does not update download hashes.
 * @param {EditorDocument} doc
 * @param {'apt'|'air'|'both'} [side='both']
 * @returns {EditorDocument}
 */
export function markClean(doc, side = 'both') {
  if (side === 'apt' || side === 'both') doc.aptDirty = false;
  if (side === 'air' || side === 'both') doc.airDirty = false;
  return doc;
}

/**
 * After a Blob download is initiated for formatted text, store hash and
 * clear dirty only if the side is still at that text.
 * @param {EditorDocument} doc
 * @param {'apt'|'air'} side
 * @param {string} formattedText
 * @returns {EditorDocument}
 */
export function noteDownload(doc, side, formattedText) {
  const h = hashText(formattedText);
  if (side === 'apt') {
    doc.lastAptDownloadHash = h;
    doc.aptDirty = false;
  } else if (side === 'air') {
    doc.lastAirDownloadHash = h;
    doc.airDirty = false;
  }
  return doc;
}

/**
 * Recompute dirty flags from current formatted text vs last download hash.
 * Call after mutations when you already have formatted text.
 * @param {EditorDocument} doc
 * @param {'apt'|'air'} side
 * @param {string} formattedText
 * @returns {EditorDocument}
 */
export function syncDirtyFromHash(doc, side, formattedText) {
  const h = hashText(formattedText);
  if (side === 'apt') {
    doc.aptDirty = doc.lastAptDownloadHash == null || doc.lastAptDownloadHash !== h;
  } else if (side === 'air') {
    doc.airDirty = doc.lastAirDownloadHash == null || doc.lastAirDownloadHash !== h;
  }
  return doc;
}

/**
 * Set editor mode (select / park / taxi / runway / hold / aircraft).
 * @param {EditorDocument} doc
 * @param {string} mode
 * @returns {EditorDocument}
 */
export function setMode(doc, mode) {
  if (EDITOR_MODES.includes(mode)) {
    doc.mode = mode;
  }
  return doc;
}

/**
 * Replace airport geometry.
 * @param {EditorDocument} doc
 * @param {Airport|null} airport
 * @param {{ filename?: string|null, errors?: string[], markDirty?: boolean }} [opts]
 * @returns {EditorDocument}
 */
export function setAirport(doc, airport, opts = {}) {
  doc.airport = airport;
  if (airport) ensureSurfaceIds(doc, airport);
  if (opts.filename !== undefined) doc.aptFilename = opts.filename;
  if (opts.errors !== undefined) doc.aptErrors = opts.errors.slice();
  if (opts.markDirty !== false) doc.aptDirty = true;
  return doc;
}

/**
 * Replace aircraft list.
 * @param {EditorDocument} doc
 * @param {Aircraft[]} aircraft
 * @param {{ filename?: string|null, errors?: string[], markDirty?: boolean }} [opts]
 * @returns {EditorDocument}
 */
export function setAircraft(doc, aircraft, opts = {}) {
  doc.aircraft = Array.isArray(aircraft) ? aircraft.slice() : [];
  if (opts.filename !== undefined) doc.airFilename = opts.filename;
  if (opts.errors !== undefined) doc.airErrors = opts.errors.slice();
  if (opts.markDirty !== false) doc.airDirty = true;
  return doc;
}

/**
 * Ensure airport exists (empty defaults) for drawing into.
 * Marks dirty when creating new.
 * @param {EditorDocument} doc
 * @returns {Airport}
 */
export function ensureAirport(doc) {
  if (!doc.airport) {
    doc.airport = createEmptyAirport();
    doc.aptDirty = true;
  }
  return doc.airport;
}

/**
 * Assign client-only surface ids if missing.
 * @param {EditorDocument} doc
 * @param {Airport} airport
 */
function ensureSurfaceIds(doc, airport) {
  if (!airport || !Array.isArray(airport.surfaces)) return;
  for (const s of airport.surfaces) {
    if (!s.id) {
      s.id = nextSurfaceId(doc);
    }
  }
}

/**
 * @param {EditorDocument} doc
 * @returns {string}
 */
export function nextSurfaceId(doc) {
  const n = doc._nextSurfaceId || 1;
  doc._nextSurfaceId = n + 1;
  return `s${n}`;
}

/**
 * Next auto parking name P1, P2, … (skips names already used).
 * @param {Airport|null|undefined} airport
 * @returns {string}
 */
export function nextParkingName(airport) {
  const used = new Set();
  for (const s of airport?.surfaces || []) {
    if (s.kind === SurfaceParking) used.add(String(s.name || '').toUpperCase());
  }
  let i = 1;
  while (used.has(`P${i}`)) i++;
  return `P${i}`;
}

/**
 * Next auto taxi/hold name A, B, … then A1…
 * Prefer short names; for holds use H1, H2…
 * @param {Airport|null|undefined} airport
 * @param {'TAXIWAY'|'HOLD'} kind
 * @returns {string}
 */
export function nextPathName(airport, kind) {
  const used = new Set();
  for (const s of airport?.surfaces || []) {
    if (s.kind === SurfaceTaxiway || s.kind === SurfaceHold) {
      used.add(String(s.name || '').toUpperCase());
    }
  }
  if (kind === SurfaceHold) {
    let i = 1;
    while (used.has(`H${i}`)) i++;
    return `H${i}`;
  }
  // Taxiway: A, B, … Z, then A1…
  for (let c = 65; c <= 90; c++) {
    const name = String.fromCharCode(c);
    if (!used.has(name)) return name;
  }
  let i = 1;
  while (used.has(`A${i}`)) i++;
  return `A${i}`;
}

/**
 * Next unique aircraft callsign N001, N002…
 * @param {Aircraft[]} aircraft
 * @returns {string}
 */
export function nextAircraftCallsign(aircraft) {
  const used = new Set(
    (aircraft || []).map((a) => String(a.callsign || '').toUpperCase()),
  );
  let i = 1;
  while (used.has(`N${String(i).padStart(3, '0')}`)) i++;
  return `N${String(i).padStart(3, '0')}`;
}

/**
 * Default aircraft fields when placing on map.
 * dep/arr from airport ICAO; alt from field elev; heading from opts or 0.
 * @param {Airport|null|undefined} airport
 * @param {{ lat: number, lon: number, heading?: number, callsign?: string }} pos
 * @param {Aircraft[]} [existing]
 * @returns {Aircraft}
 */
export function createDefaultAircraft(airport, pos, existing = []) {
  const icao = String(airport?.icao || '').toUpperCase();
  const elev = Number(airport?.fieldElev) || 0;
  return {
    callsign: pos.callsign || nextAircraftCallsign(existing),
    type: 'C172',
    engine: EnginePiston,
    rules: RulesVFR,
    dep: icao,
    arr: icao,
    cruiseAlt: 0,
    route: '',
    remarks: '',
    squawk: '1200',
    xpdrMode: XPDRModeNormal,
    lat: pos.lat,
    lon: pos.lon,
    alt: elev,
    speed: 0,
    heading: Number.isFinite(pos.heading) ? Number(pos.heading) : 0,
  };
}

/**
 * @param {Airport|null|undefined} airport
 * @returns {Surface[]}
 */
export function parkingSurfaces(airport) {
  if (!airport || !Array.isArray(airport.surfaces)) return [];
  return airport.surfaces.filter((s) => s.kind === SurfaceParking);
}

/**
 * Add a surface; marks APT dirty. Returns surface index.
 * @param {EditorDocument} doc
 * @param {Surface} surface
 * @returns {number}
 */
export function addSurface(doc, surface) {
  const apt = ensureAirport(doc);
  const s = { ...surface };
  if (!s.id) s.id = nextSurfaceId(doc);
  if (!Array.isArray(s.points)) s.points = [];
  apt.surfaces.push(s);
  doc.aptDirty = true;
  return apt.surfaces.length - 1;
}

/**
 * Update surface fields at index (shallow merge). Marks APT dirty.
 * @param {EditorDocument} doc
 * @param {number} index
 * @param {Partial<Surface>} patch
 * @returns {boolean}
 */
export function updateSurface(doc, index, patch) {
  const s = doc.airport?.surfaces?.[index];
  if (!s) return false;
  Object.assign(s, patch);
  doc.aptDirty = true;
  return true;
}

/**
 * Delete surface at index. Clears selection if it pointed at this surface.
 * @param {EditorDocument} doc
 * @param {number} index
 * @returns {boolean}
 */
export function deleteSurface(doc, index) {
  if (!doc.airport?.surfaces || index < 0 || index >= doc.airport.surfaces.length) {
    return false;
  }
  doc.airport.surfaces.splice(index, 1);
  doc.aptDirty = true;
  if (doc.selection?.type === 'surface') {
    if (doc.selection.index === index) doc.selection = null;
    else if (doc.selection.index > index) {
      doc.selection = { type: 'surface', index: doc.selection.index - 1 };
    }
  }
  return true;
}

/**
 * Set / move a vertex. Marks APT dirty.
 * @param {EditorDocument} doc
 * @param {number} surfaceIndex
 * @param {number} vertexIndex
 * @param {Point} point
 * @returns {boolean}
 */
export function setVertex(doc, surfaceIndex, vertexIndex, point) {
  const s = doc.airport?.surfaces?.[surfaceIndex];
  if (!s || !s.points || vertexIndex < 0 || vertexIndex >= s.points.length) return false;
  s.points[vertexIndex] = { lat: point.lat, lon: point.lon };
  doc.aptDirty = true;
  return true;
}

/**
 * Insert vertex after index (or at 0 if afterIndex < 0). Marks APT dirty.
 * @param {EditorDocument} doc
 * @param {number} surfaceIndex
 * @param {number} afterIndex - insert after this index (-1 = front)
 * @param {Point} point
 * @returns {boolean}
 */
export function insertVertex(doc, surfaceIndex, afterIndex, point) {
  const s = doc.airport?.surfaces?.[surfaceIndex];
  if (!s) return false;
  if (!Array.isArray(s.points)) s.points = [];
  const at = Math.max(0, Math.min(s.points.length, afterIndex + 1));
  s.points.splice(at, 0, { lat: point.lat, lon: point.lon });
  doc.aptDirty = true;
  return true;
}

/**
 * Delete vertex if remaining points stay above min for kind.
 * @param {EditorDocument} doc
 * @param {number} surfaceIndex
 * @param {number} vertexIndex
 * @returns {{ ok: boolean, reason?: string }}
 */
export function deleteVertex(doc, surfaceIndex, vertexIndex) {
  const s = doc.airport?.surfaces?.[surfaceIndex];
  if (!s || !s.points) return { ok: false, reason: 'no surface' };
  if (vertexIndex < 0 || vertexIndex >= s.points.length) {
    return { ok: false, reason: 'bad index' };
  }
  const min = minPointsForKind(s.kind);
  if (s.points.length <= min) {
    return { ok: false, reason: `need at least ${min} point(s)` };
  }
  s.points.splice(vertexIndex, 1);
  doc.aptDirty = true;
  return { ok: true };
}

/**
 * Add aircraft; marks AIR dirty. Returns index.
 * @param {EditorDocument} doc
 * @param {Aircraft} ac
 * @returns {number}
 */
export function addAircraft(doc, ac) {
  if (!Array.isArray(doc.aircraft)) doc.aircraft = [];
  doc.aircraft.push({ ...ac });
  doc.airDirty = true;
  return doc.aircraft.length - 1;
}

/**
 * Patch aircraft fields. Marks AIR dirty.
 * @param {EditorDocument} doc
 * @param {number} index
 * @param {Partial<Aircraft>} patch
 * @returns {boolean}
 */
export function updateAircraft(doc, index, patch) {
  const ac = doc.aircraft?.[index];
  if (!ac) return false;
  Object.assign(ac, patch);
  doc.airDirty = true;
  return true;
}

/**
 * Delete aircraft at index.
 * @param {EditorDocument} doc
 * @param {number} index
 * @returns {boolean}
 */
export function deleteAircraft(doc, index) {
  if (!doc.aircraft || index < 0 || index >= doc.aircraft.length) return false;
  doc.aircraft.splice(index, 1);
  doc.airDirty = true;
  if (doc.selection?.type === 'aircraft') {
    if (doc.selection.index === index) doc.selection = null;
    else if (doc.selection.index > index) {
      doc.selection = { type: 'aircraft', index: doc.selection.index - 1 };
    }
  }
  return true;
}

/**
 * Snap aircraft lat/lon to parking surface first point.
 * Optional heading left unchanged unless opts.setHeading provided.
 * @param {EditorDocument} doc
 * @param {number} aircraftIndex
 * @param {number} parkingSurfaceIndex - index into airport.surfaces
 * @returns {boolean}
 */
export function snapAircraftToParking(doc, aircraftIndex, parkingSurfaceIndex) {
  const ac = doc.aircraft?.[aircraftIndex];
  const s = doc.airport?.surfaces?.[parkingSurfaceIndex];
  if (!ac || !s || s.kind !== SurfaceParking) return false;
  const p = s.points?.[0];
  if (!p || !Number.isFinite(p.lat) || !Number.isFinite(p.lon)) return false;
  ac.lat = p.lat;
  ac.lon = p.lon;
  if (doc.airport && Number.isFinite(doc.airport.fieldElev)) {
    ac.alt = doc.airport.fieldElev;
  }
  doc.airDirty = true;
  return true;
}

/**
 * Patch airport header fields. Marks APT dirty.
 * @param {EditorDocument} doc
 * @param {Partial<Airport>} patch
 * @returns {boolean}
 */
export function updateAirportHeaders(doc, patch) {
  const apt = ensureAirport(doc);
  // Do not replace surfaces via this helper.
  const { surfaces: _ignore, ...rest } = /** @type {any} */ (patch);
  Object.assign(apt, rest);
  doc.aptDirty = true;
  return true;
}

/**
 * Reset both sides to empty (New).
 * @param {EditorDocument} doc
 * @returns {EditorDocument}
 */
export function resetDocument(doc) {
  const empty = createEmptyDocument();
  Object.assign(doc, empty);
  return doc;
}

/**
 * Find a surface by name (case-insensitive).
 * For runways, matches either end designator or the combined "A/B" name.
 * @param {Airport|null|undefined} airport
 * @param {string} name
 * @returns {Surface|null}
 */
export function findSurface(airport, name) {
  if (!airport || !Array.isArray(airport.surfaces)) return null;
  const want = String(name).toUpperCase();
  for (const s of airport.surfaces) {
    if (s.kind === SurfaceRunway) {
      if (s.rwyA === want || s.rwyB === want || s.name === want) return s;
      continue;
    }
    if (String(s.name).toUpperCase() === want) return s;
  }
  return null;
}

/**
 * Whether any parking exists (snap UI enable).
 * @param {Airport|null|undefined} airport
 * @returns {boolean}
 */
export function hasParking(airport) {
  return parkingSurfaces(airport).length > 0;
}
