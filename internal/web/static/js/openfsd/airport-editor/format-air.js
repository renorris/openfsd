/**
 * Format aircraft rows → TWRTrainer-compatible .air text.
 * Port of pkg/twrfiles.FormatAIR — normative Format contract (16 fields).
 */

/**
 * Serialize aircraft rows. Empty list → empty string (no trailing newline).
 * @param {import('./model.js').Aircraft[]} rows
 * @returns {string}
 */
export function formatAIR(rows) {
  if (!Array.isArray(rows) || rows.length === 0) return '';
  const lines = rows.map(writeAircraftLine);
  return `${lines.join('\n')}\n`;
}

/**
 * @param {import('./model.js').Aircraft} ac
 * @returns {string}
 */
function writeAircraftLine(ac) {
  const fields = [
    String(ac.callsign ?? '').toUpperCase(),
    String(ac.type ?? '').toUpperCase(),
    String(ac.engine ?? '').toUpperCase(),
    String(ac.rules ?? '').toUpperCase(),
    String(ac.dep ?? '').toUpperCase(),
    String(ac.arr ?? '').toUpperCase(),
    String(Math.trunc(Number(ac.cruiseAlt) || 0)),
    ac.route ?? '',
    ac.remarks ?? '',
    ac.squawk ?? '',
    String(ac.xpdrMode ?? '').toUpperCase(),
    formatCoord6(ac.lat),
    formatCoord6(ac.lon),
    formatAirCompactFloat(ac.alt),
    formatAirCompactFloat(ac.speed),
    formatAirCompactFloat(ac.heading),
  ];
  return fields.join(':');
}

/**
 * Always six fractional digits (Go %.6f).
 * @param {number} v
 */
function formatCoord6(v) {
  return Number(v).toFixed(6);
}

/**
 * Integer form when whole; else shortest decimal (Go FormatFloat 'f' -1).
 * @param {number} v
 */
export function formatAirCompactFloat(v) {
  const n = Number(v);
  if (!Number.isFinite(n)) return '0';
  if (n === Math.trunc(n)) {
    return String(Math.trunc(n));
  }
  let s = String(n);
  if (/e/i.test(s)) {
    s = n.toFixed(16).replace(/(\.\d*?)0+$/, '$1').replace(/\.$/, '');
  }
  return s;
}
