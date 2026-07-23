/**
 * Parse TWRTrainer-compatible .apt text.
 * Port of pkg/twrfiles.ParseAPT — error messages match Go where practical.
 */

import {
  DefaultRegistration,
  DEFAULT_PATTERN_SIZE,
  DEFAULT_INIT_CLIMB_PROPS,
  DEFAULT_INIT_CLIMB_JETS,
  SurfaceParking,
  SurfaceRunway,
  SurfaceTaxiway,
  SurfaceHold,
} from './model.js';

/** Shared taxiway/hold name namespace for duplicate detection. */
const PATH_DUP_KEY = 'PATH';

/**
 * Parse APT text.
 * @param {string} text
 * @returns {{ airport: import('./model.js').Airport, errors: string[] }}
 */
export function parseAPT(text) {
  const airport = {
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
  /** @type {string[]} */
  const errors = [];
  /** @type {import('./model.js').Surface|null} */
  let cur = null;
  /** @type {Map<string, number>} */
  const seen = new Map();

  const lines = String(text ?? '').split('\n');
  for (let i = 0; i < lines.length; i++) {
    const lineno = i + 1;
    // Trim also strips trailing \r from CRLF lines.
    const line = lines[i].trim();
    if (line === '' || line.startsWith(';')) continue;
    const low = line.toLowerCase();

    if (low.startsWith('icao=')) {
      airport.icao = line.slice('icao='.length).trim().toUpperCase();
      if (airport.icao.length !== 4) {
        errors.push('ICAO code must be exactly 4 characters.');
      }
      continue;
    }
    if (low.startsWith('magnetic variation=')) {
      const v = parseFloatField(line);
      if (v === null) {
        errors.push(`Invalid magnetic variation on line ${lineno}`);
        continue;
      }
      airport.magVar = v;
      continue;
    }
    if (low.startsWith('field elevation=')) {
      const v = parseFloatField(line);
      if (v === null) {
        errors.push(`Invalid field elevation on line ${lineno}`);
        continue;
      }
      airport.fieldElev = v;
      continue;
    }
    if (low.startsWith('pattern elevation=')) {
      const v = parseFloatField(line);
      if (v === null) {
        errors.push(`Invalid pattern elevation on line ${lineno}`);
        continue;
      }
      airport.patternElev = v;
      continue;
    }
    if (low.startsWith('pattern size=')) {
      const v = parseFloatField(line);
      if (v === null) {
        errors.push(`Invalid pattern size on line ${lineno}`);
        continue;
      }
      airport.patternSize = v;
      continue;
    }
    if (low.startsWith('initial climb props=')) {
      const v = parseFloatField(line);
      if (v === null) {
        errors.push(`Invalid initial climb props on line ${lineno}`);
        continue;
      }
      airport.initClimbProps = v;
      continue;
    }
    if (low.startsWith('initial climb jets=')) {
      const v = parseFloatField(line);
      if (v === null) {
        errors.push(`Invalid initial climb jets on line ${lineno}`);
        continue;
      }
      airport.initClimbJets = v;
      continue;
    }
    if (low.startsWith('jet airlines=')) {
      const idx = line.indexOf('=');
      const val = line.slice(idx + 1).trim();
      airport.jetAirlines = val;
      if (!isValidAirlineList(val)) {
        errors.push(`Invalid list of jet airlines on line ${lineno}`);
      }
      continue;
    }
    if (low.startsWith('turboprop airlines=')) {
      const idx = line.indexOf('=');
      const val = line.slice(idx + 1).trim();
      airport.turboAirlines = val;
      if (!isValidAirlineList(val)) {
        errors.push(`Invalid list of turboprop airlines on line ${lineno}`);
      }
      continue;
    }
    if (low.startsWith('registration=')) {
      const idx = line.indexOf('=');
      const val = line.slice(idx + 1).trim();
      airport.registration = val;
      if (!isValidRegistration(val)) {
        errors.push(`Invalid registration prefix on line ${lineno}`);
      }
      continue;
    }

    // Runway-only options.
    if (low.startsWith('turnoff=') && cur != null && cur.kind === SurfaceRunway) {
      const val = line.slice('turnoff='.length).trim().toLowerCase();
      if (val === 'left') {
        cur.turnoffLeft = true;
      } else if (val === 'right') {
        cur.turnoffLeft = false;
      } else {
        errors.push(`Invalid turnoff direction found on line ${lineno}`);
      }
      continue;
    }
    {
      const disp = parseDisplacedThreshold(line);
      if (disp) {
        if (cur != null && cur.kind === SurfaceRunway) {
          cur.dispA = disp.da;
          cur.dispB = disp.db;
        } else {
          errors.push(`Unknown line format found on line ${lineno}`);
        }
        continue;
      }
    }

    // Section headers.
    {
      const name = matchSection(line, 'PARKING');
      if (name !== null) {
        if (!isWordName(name)) {
          errors.push(`Unknown line format found on line ${lineno}`);
          cur = null;
          continue;
        }
        const s = {
          kind: SurfaceParking,
          name: name.toUpperCase(),
          points: [],
        };
        const key = `${SurfaceParking}:${s.name}`;
        if (seen.has(key)) {
          errors.push(
            `Duplicate parking ${s.name} (first on line ${seen.get(key)}, again on line ${lineno})`,
          );
        } else {
          seen.set(key, lineno);
        }
        airport.surfaces.push(s);
        cur = s;
        continue;
      }
    }
    {
      const rwy = matchRunway(line);
      if (rwy) {
        const s = {
          kind: SurfaceRunway,
          name: `${rwy.a.toUpperCase()}/${rwy.b.toUpperCase()}`,
          rwyA: rwy.a.toUpperCase(),
          rwyB: rwy.b.toUpperCase(),
          dispA: 0,
          dispB: 0,
          turnoffLeft: true,
          points: [],
        };
        const key = `${SurfaceRunway}:${s.name}`;
        if (seen.has(key)) {
          errors.push(
            `Duplicate runway ${s.name} (first on line ${seen.get(key)}, again on line ${lineno})`,
          );
        } else {
          seen.set(key, lineno);
        }
        airport.surfaces.push(s);
        cur = s;
        continue;
      }
    }
    {
      const name = matchSection(line, 'TAXIWAY');
      if (name !== null) {
        if (!isTaxiHoldName(name)) {
          errors.push(`Unknown line format found on line ${lineno}`);
          cur = null;
          continue;
        }
        const s = {
          kind: SurfaceTaxiway,
          name: name.toUpperCase(),
          points: [],
        };
        const key = `${PATH_DUP_KEY}:${s.name}`;
        if (seen.has(key)) {
          errors.push(
            `Duplicate taxiway or hold point definition ${s.name} (first on line ${seen.get(key)}, again on line ${lineno})`,
          );
        } else {
          seen.set(key, lineno);
        }
        airport.surfaces.push(s);
        cur = s;
        continue;
      }
    }
    {
      const name = matchSection(line, 'HOLD');
      if (name !== null) {
        if (!isTaxiHoldName(name)) {
          errors.push(`Unknown line format found on line ${lineno}`);
          cur = null;
          continue;
        }
        const s = {
          kind: SurfaceHold,
          name: name.toUpperCase(),
          points: [],
        };
        const key = `${PATH_DUP_KEY}:${s.name}`;
        if (seen.has(key)) {
          errors.push(
            `Duplicate taxiway or hold point definition ${s.name} (first on line ${seen.get(key)}, again on line ${lineno})`,
          );
        } else {
          seen.set(key, lineno);
        }
        airport.surfaces.push(s);
        cur = s;
        continue;
      }
    }

    // Waypoint: lat lon with required decimal point.
    {
      const pt = parsePoint(line);
      if (pt) {
        if (cur == null) {
          errors.push(`Unknown line format found on line ${lineno}`);
          continue;
        }
        cur.points.push(pt);
        continue;
      }
    }

    errors.push(`Unknown line format found on line ${lineno}`);
  }

  // Post-validation: waypoint counts.
  for (const s of airport.surfaces) {
    switch (s.kind) {
      case SurfaceParking:
        if (s.points.length === 0) {
          errors.push(`Parking area ${s.name} has no waypoint defined.`);
        } else if (s.points.length > 1) {
          errors.push(`Extra waypoint found in parking section ${s.name}.`);
        }
        break;
      case SurfaceRunway:
        if (s.points.length < 2) {
          errors.push(`Runway ${s.name} does not have at least two waypoints defined.`);
        }
        break;
      case SurfaceHold:
        if (s.points.length === 0) {
          errors.push(`Hold ${s.name} has no waypoint defined.`);
        } else if (s.points.length > 1) {
          errors.push(`Extra waypoint found in hold section ${s.name}.`);
        }
        break;
      case SurfaceTaxiway:
        if (s.points.length < 2) {
          errors.push(`Taxiway ${s.name} does not have at least two waypoints defined.`);
        }
        break;
    }
  }

  return { airport, errors };
}

/**
 * @param {string} line
 * @returns {number|null}
 */
function parseFloatField(line) {
  const idx = line.indexOf('=');
  if (idx < 0) return null;
  const s = line.slice(idx + 1).trim();
  if (s === '' || !isParseableFloat(s)) return null;
  const v = Number(s);
  if (!Number.isFinite(v)) return null;
  return v;
}

/**
 * Match TWRTrainer displaced threshold=(\d+)/(\d+).
 * @param {string} line
 * @returns {{ da: number, db: number }|null}
 */
function parseDisplacedThreshold(line) {
  const low = line.toLowerCase();
  const prefix = 'displaced threshold=';
  if (!low.startsWith(prefix)) return null;
  const eq = line.indexOf('=');
  const rest = eq >= 0 ? line.slice(eq + 1).trim() : '';
  const parts = rest.split('/');
  if (parts.length !== 2) return null;
  const aStr = parts[0].trim();
  const bStr = parts[1].trim();
  if (!isDigits(aStr) || !isDigits(bStr)) return null;
  return { da: Number(aStr), db: Number(bStr) };
}

/** @param {string} s */
function isDigits(s) {
  if (s === '') return false;
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c < 48 || c > 57) return false;
  }
  return true;
}

/** Single word character registration. */
function isValidRegistration(s) {
  return s.length === 1 && isWordName(s);
}

/**
 * Empty or 2–3 char \w prefixes separated by commas (trailing comma allowed).
 * @param {string} s
 */
function isValidAirlineList(s) {
  if (s === '') return true;
  const parts = s.split(',');
  for (let i = 0; i < parts.length; i++) {
    const p = parts[i];
    if (p === '') {
      if (i === parts.length - 1) continue;
      return false;
    }
    if (p.length < 2 || p.length > 3 || !isWordName(p)) return false;
  }
  return true;
}

/**
 * Match "[KIND name]" case-insensitively.
 * @param {string} line
 * @param {string} kind
 * @returns {string|null}
 */
function matchSection(line, kind) {
  if (line.length < 3 || line[0] !== '[' || line[line.length - 1] !== ']') return null;
  const inner = line.slice(1, -1);
  const parts = inner.trim().split(/\s+/).filter(Boolean);
  if (parts.length !== 2) return null;
  if (parts[0].toUpperCase() !== kind.toUpperCase()) return null;
  return parts[1];
}

/**
 * Match [RUNWAY a/b].
 * @param {string} line
 * @returns {{ a: string, b: string }|null}
 */
function matchRunway(line) {
  if (line.length < 3 || line[0] !== '[' || line[line.length - 1] !== ']') return null;
  const inner = line.slice(1, -1);
  const parts = inner.trim().split(/\s+/).filter(Boolean);
  if (parts.length !== 2) return null;
  if (parts[0].toUpperCase() !== 'RUNWAY') return null;
  const ends = parts[1].split('/');
  if (ends.length !== 2) return null;
  if (!isRunwayDesignator(ends[0]) || !isRunwayDesignator(ends[1])) return null;
  return { a: ends[0], b: ends[1] };
}

/**
 * Matches (?:[1-2]\d|3[0-6]|[1-9])[LRC]?
 * @param {string} s
 */
export function isRunwayDesignator(s) {
  if (!s) return false;
  let t = s.toUpperCase();
  const last = t[t.length - 1];
  if (last === 'L' || last === 'R' || last === 'C') {
    t = t.slice(0, -1);
  }
  if (t === '') return false;
  const n = Number(t);
  if (!Number.isInteger(n) || n < 1 || n > 36) return false;
  if (t.length > 1 && t[0] === '0') return false;
  // Reject non-digit bodies (Number accepts whitespace / partial).
  if (!/^\d+$/.test(t)) return false;
  return true;
}

/**
 * Matches \w+ (ASCII letters, digits, underscore).
 * @param {string} s
 */
export function isWordName(s) {
  if (!s) return false;
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    const ok =
      (c >= 97 && c <= 122) ||
      (c >= 65 && c <= 90) ||
      (c >= 48 && c <= 57) ||
      c === 95;
    if (!ok) return false;
  }
  return true;
}

/**
 * Matches [A-Z]+\d* (letters then optional digits), case-insensitive.
 * @param {string} s
 */
export function isTaxiHoldName(s) {
  if (!s) return false;
  let i = 0;
  for (; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if ((c >= 97 && c <= 122) || (c >= 65 && c <= 90)) continue;
    break;
  }
  if (i === 0) return false;
  for (; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c < 48 || c > 57) return false;
  }
  return true;
}

/**
 * @param {string} line
 * @returns {{ lat: number, lon: number }|null}
 */
function parsePoint(line) {
  const parts = line.trim().split(/\s+/).filter(Boolean);
  if (parts.length !== 2) return null;
  if (!hasDecimalPoint(parts[0]) || !hasDecimalPoint(parts[1])) return null;
  const lat = Number(parts[0]);
  const lon = Number(parts[1]);
  if (!Number.isFinite(lat) || !Number.isFinite(lon)) return null;
  return { lat, lon };
}

/** @param {string} s */
function hasDecimalPoint(s) {
  if (!s) return false;
  let start = 0;
  if (s[0] === '-') start = 1;
  const rest = s.slice(start);
  const dot = rest.indexOf('.');
  if (dot < 0) return false;
  const before = rest.slice(0, dot);
  const after = rest.slice(dot + 1);
  if (before === '' || after === '') return false;
  if (!isDigits(before) || !isDigits(after)) return false;
  return true;
}

/**
 * Accept Go strconv.ParseFloat-like decimal forms (reject empty / pure junk).
 * @param {string} s
 */
function isParseableFloat(s) {
  // Allow optional sign, digits, optional decimal, optional exponent.
  return /^[+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?$/.test(s);
}
