/**
 * Format Airport → TWRTrainer-compatible .apt text.
 * Port of pkg/twrfiles.FormatAPT — normative Format contract.
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

/**
 * Serialize an Airport to .apt text (LF, trailing newline, all headers).
 * @param {import('./model.js').Airport} a
 * @returns {string}
 */
export function formatAPT(a) {
  const icao = String(a?.icao ?? '').toUpperCase();
  let patternSize = a?.patternSize ?? 0;
  if (patternSize === 0) patternSize = DEFAULT_PATTERN_SIZE;
  let initProps = a?.initClimbProps ?? 0;
  if (initProps === 0) initProps = DEFAULT_INIT_CLIMB_PROPS;
  let initJets = a?.initClimbJets ?? 0;
  if (initJets === 0) initJets = DEFAULT_INIT_CLIMB_JETS;
  let reg = a?.registration ?? '';
  if (reg === '') reg = DefaultRegistration;

  const parts = [];
  parts.push(`icao=${icao}`);
  parts.push(`magnetic variation=${formatHeaderFloat(a?.magVar ?? 0)}`);
  parts.push(`field elevation=${formatHeaderFloat(a?.fieldElev ?? 0)}`);
  parts.push(`pattern elevation=${formatHeaderFloat(a?.patternElev ?? 0)}`);
  parts.push(`pattern size=${formatHeaderFloat(patternSize)}`);
  parts.push(`initial climb props=${formatHeaderFloat(initProps)}`);
  parts.push(`initial climb jets=${formatHeaderFloat(initJets)}`);
  parts.push(`jet airlines=${a?.jetAirlines ?? ''}`);
  parts.push(`turboprop airlines=${a?.turboAirlines ?? ''}`);
  parts.push(`registration=${reg}`);
  // Blank line after headers.
  parts.push('');

  const surfaces = Array.isArray(a?.surfaces) ? a.surfaces : [];
  for (let i = 0; i < surfaces.length; i++) {
    if (i > 0) parts.push('');
    writeSurface(parts, surfaces[i]);
  }

  // parts includes a trailing empty line body after headers so empty airports
  // become "...registration=N\n\n". Always terminate with LF (Go contract).
  return `${parts.join('\n')}\n`;
}

/**
 * @param {string[]} parts
 * @param {import('./model.js').Surface} s
 */
function writeSurface(parts, s) {
  switch (s.kind) {
    case SurfaceRunway: {
      let name = s.name;
      if ((!name || name === '') && (s.rwyA || s.rwyB)) {
        name = `${s.rwyA}/${s.rwyB}`;
      }
      parts.push(`[RUNWAY ${name}]`);
      const da = Math.trunc(s.dispA ?? 0);
      const db = Math.trunc(s.dispB ?? 0);
      parts.push(`displaced threshold=${da}/${db}`);
      // Match parse default (left when omitted). Intentionally not Go's zero-value
      // bool (false → right): hand-built partial surfaces without turnoffLeft
      // should still emit the TWRTrainer/parse default of left.
      const turnoffLeft = s.turnoffLeft ?? true;
      parts.push(turnoffLeft ? 'turnoff=left' : 'turnoff=right');
      break;
    }
    case SurfaceParking:
      parts.push(`[PARKING ${s.name}]`);
      break;
    case SurfaceTaxiway:
      parts.push(`[TAXIWAY ${s.name}]`);
      break;
    case SurfaceHold:
      parts.push(`[HOLD ${s.name}]`);
      break;
    default:
      parts.push(`[${s.kind} ${s.name}]`);
      break;
  }
  const pts = Array.isArray(s.points) ? s.points : [];
  for (const p of pts) {
    parts.push(`${formatCoord(p.lat)} ${formatCoord(p.lon)}`);
  }
}

/**
 * Always six fractional digits (Go %.6f). Non-finite → 0.000000 (defensive;
 * callers should validate coords; matches formatHeaderFloat finite guard).
 * @param {number} v
 */
export function formatCoord(v) {
  const n = Number(v);
  if (!Number.isFinite(n)) return '0.000000';
  return n.toFixed(6);
}

/**
 * Compact deterministic header float (Go strconv.FormatFloat 'f' -1).
 * @param {number} v
 */
export function formatHeaderFloat(v) {
  const n = Number(v);
  if (!Number.isFinite(n)) return '0';
  if (Object.is(n, -0) || n === 0) return '0';
  if (Number.isInteger(n) || Math.trunc(n) === n) {
    return String(Math.trunc(n));
  }
  // Prefer shortest decimal without scientific notation for airport ranges.
  let s = String(n);
  if (/e/i.test(s)) {
    s = n.toFixed(16).replace(/(\.\d*?)0+$/, '$1').replace(/\.$/, '');
  }
  return s;
}
