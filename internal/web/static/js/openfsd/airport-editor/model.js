/**
 * Pure document model helpers for the airport editor.
 * Mirrors pkg/twrfiles types (camelCase field names).
 * No DOM / window access — Node-testable.
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
 * @property {string} mode
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
    mode: 'select',
  };
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
 * Replace airport geometry and mark APT dirty.
 * @param {EditorDocument} doc
 * @param {Airport|null} airport
 * @param {{ filename?: string|null, errors?: string[], markDirty?: boolean }} [opts]
 * @returns {EditorDocument}
 */
export function setAirport(doc, airport, opts = {}) {
  doc.airport = airport;
  if (opts.filename !== undefined) doc.aptFilename = opts.filename;
  if (opts.errors !== undefined) doc.aptErrors = opts.errors.slice();
  if (opts.markDirty !== false) doc.aptDirty = true;
  return doc;
}

/**
 * Replace aircraft list and mark AIR dirty.
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
