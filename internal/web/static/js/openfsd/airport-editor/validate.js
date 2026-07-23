/**
 * Validation helpers for the airport editor.
 * Runs parse and collects errors; optional soft cross-file warnings.
 */

import { parseAPT } from './parse-apt.js';
import { parseAIR } from './parse-air.js';
import { SurfaceParking, SurfaceRunway } from './model.js';

/**
 * Validate .apt text via parseAPT.
 * @param {string} text
 * @returns {{ airport: import('./model.js').Airport, errors: string[], ok: boolean }}
 */
export function validateAPT(text) {
  const { airport, errors } = parseAPT(text);
  return { airport, errors, ok: errors.length === 0 };
}

/**
 * Validate .air text via parseAIR.
 * @param {string} text
 * @returns {{ aircraft: import('./model.js').Aircraft[], errors: string[], ok: boolean }}
 */
export function validateAIR(text) {
  const { aircraft, errors } = parseAIR(text);
  return { aircraft, errors, ok: errors.length === 0 };
}

/**
 * Soft cross-file warnings (not parse errors).
 * @param {import('./model.js').Airport|null|undefined} airport
 * @param {import('./model.js').Aircraft[]|null|undefined} aircraft
 * @returns {string[]}
 */
export function crossFileWarnings(airport, aircraft) {
  /** @type {string[]} */
  const warns = [];
  const rows = Array.isArray(aircraft) ? aircraft : [];
  if (!airport) {
    if (rows.length > 0) {
      warns.push('Aircraft loaded without airport geometry.');
    }
    return warns;
  }
  const icao = String(airport.icao || '').toUpperCase();
  if (icao && rows.length > 0) {
    for (const ac of rows) {
      const dep = String(ac.dep || '').toUpperCase();
      if (dep && dep !== icao) {
        warns.push(`Aircraft ${ac.callsign}: dep ${dep} ≠ airport ICAO ${icao}.`);
      }
    }
  }
  // Soft: aircraft far from any parking / field (approx > 50 NM from first parking or runway).
  const anchor = fieldAnchor(airport);
  if (anchor && rows.length > 0) {
    for (const ac of rows) {
      const nm = haversineNM(anchor.lat, anchor.lon, ac.lat, ac.lon);
      if (nm > 50) {
        warns.push(
          `Aircraft ${ac.callsign}: position ~${nm.toFixed(0)} NM from field (expected near airport).`,
        );
      }
    }
  }
  return warns;
}

/**
 * Validate both sides of an editor document model (in-memory).
 * Re-runs soft warnings; does not re-parse text.
 * @param {import('./model.js').EditorDocument} doc
 * @returns {{ aptErrors: string[], airErrors: string[], softWarnings: string[] }}
 */
export function validateDocument(doc) {
  const aptErrors = Array.isArray(doc.aptErrors) ? doc.aptErrors.slice() : [];
  const airErrors = Array.isArray(doc.airErrors) ? doc.airErrors.slice() : [];
  const softWarnings = crossFileWarnings(doc.airport, doc.aircraft);
  doc.softWarnings = softWarnings;
  return { aptErrors, airErrors, softWarnings };
}

/**
 * @param {import('./model.js').Airport} airport
 * @returns {{ lat: number, lon: number }|null}
 */
function fieldAnchor(airport) {
  const surfaces = airport.surfaces || [];
  for (const s of surfaces) {
    if (s.kind === SurfaceParking && s.points?.length) return s.points[0];
  }
  for (const s of surfaces) {
    if (s.kind === SurfaceRunway && s.points?.length) return s.points[0];
  }
  for (const s of surfaces) {
    if (s.points?.length) return s.points[0];
  }
  return null;
}

/**
 * Great-circle distance in nautical miles.
 * @param {number} lat1
 * @param {number} lon1
 * @param {number} lat2
 * @param {number} lon2
 */
function haversineNM(lat1, lon1, lat2, lon2) {
  const R = 3440.065; // Earth radius NM
  const toRad = (d) => (d * Math.PI) / 180;
  const dLat = toRad(lat2 - lat1);
  const dLon = toRad(lon2 - lon1);
  const a =
    Math.sin(dLat / 2) ** 2 +
    Math.cos(toRad(lat1)) * Math.cos(toRad(lat2)) * Math.sin(dLon / 2) ** 2;
  const c = 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
  return R * c;
}
