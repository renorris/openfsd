/**
 * Parse TWRTrainer-compatible .air scenario text.
 * Port of pkg/twrfiles.ParseAIR — error messages match Go where practical.
 */

import {
  EnginePiston,
  EngineTurboprop,
  EngineJet,
  EngineHelicopter,
  RulesVFR,
  RulesIFR,
  RulesDVFR,
  RulesSVFR,
  XPDRModeNormal,
  XPDRModeStandby,
} from './model.js';

/**
 * Parse AIR text. Valid rows are returned even when other lines fail.
 * @param {string} text
 * @returns {{ aircraft: import('./model.js').Aircraft[], errors: string[] }}
 */
export function parseAIR(text) {
  /** @type {import('./model.js').Aircraft[]} */
  const aircraft = [];
  /** @type {string[]} */
  const errors = [];
  /** @type {Map<string, number>} */
  const seen = new Map();

  const lines = String(text ?? '').split('\n');
  for (let i = 0; i < lines.length; i++) {
    const lineno = i + 1;
    const line = lines[i].trim();
    if (line === '' || line.startsWith(';')) continue;

    const f = line.split(':');
    if (f.length < 16) {
      errors.push(`Invalid number of fields found on line ${lineno}`);
      continue;
    }

    const cs = f[0].trim().toUpperCase();
    if (cs === '') {
      errors.push(`Missing callsign on line ${lineno}`);
      continue;
    }
    if (seen.has(cs)) {
      errors.push(
        `Duplicate callsign (${cs}) on line ${lineno} (first on line ${seen.get(cs)})`,
      );
      continue;
    }

    const eng = f[2].trim().toUpperCase();
    if (
      eng !== EnginePiston &&
      eng !== EngineTurboprop &&
      eng !== EngineJet &&
      eng !== EngineHelicopter
    ) {
      errors.push(`Invalid engine type on line ${lineno}. Must be P, T, J or H.`);
      continue;
    }

    const rules = f[3].trim().toUpperCase();
    if (
      rules !== RulesVFR &&
      rules !== RulesIFR &&
      rules !== RulesDVFR &&
      rules !== RulesSVFR
    ) {
      errors.push(`Invalid flight plan type on line ${lineno}. Must be V, I, D or S.`);
      continue;
    }

    const mode = f[10].trim().toUpperCase();
    if (mode !== XPDRModeNormal && mode !== XPDRModeStandby) {
      errors.push(
        `Invalid transponder mode on line ${lineno}. Must be N or S. (Normal or Standby)`,
      );
      continue;
    }

    const sqk = f[9].trim();
    if (!isSquawk(sqk)) {
      errors.push(`Invalid squawk code on line ${lineno}`);
      continue;
    }

    const cruiseRaw = f[6].trim();
    const cruiseAlt = parseNumber(cruiseRaw);
    if (cruiseAlt === null) {
      errors.push(`Invalid numeric field on line ${lineno}`);
      continue;
    }

    const lat = parseNumber(f[11].trim());
    const lon = parseNumber(f[12].trim());
    const alt = parseNumber(f[13].trim());
    const spd = parseNumber(f[14].trim());
    const hdg = parseNumber(f[15].trim());
    if (lat === null || lon === null || alt === null || spd === null || hdg === null) {
      errors.push(`Invalid numeric field on line ${lineno}`);
      continue;
    }

    const rec = {
      callsign: cs,
      type: f[1].trim().toUpperCase(),
      engine: eng,
      rules,
      dep: f[4].trim().toUpperCase(),
      arr: f[5].trim().toUpperCase(),
      // Go: int(float64) truncates toward zero.
      cruiseAlt: Math.trunc(cruiseAlt),
      route: f[7],
      remarks: f[8],
      squawk: sqk,
      xpdrMode: mode,
      lat,
      lon,
      alt,
      speed: spd,
      heading: hdg,
    };
    seen.set(cs, lineno);
    aircraft.push(rec);
  }
  return { aircraft, errors };
}

/**
 * Exactly four ASCII digits (TWRTrainer ^\d{4}$).
 * @param {string} s
 */
export function isSquawk(s) {
  if (s.length !== 4) return false;
  for (let i = 0; i < 4; i++) {
    const c = s.charCodeAt(i);
    if (c < 48 || c > 57) return false;
  }
  return true;
}

/**
 * @param {string} s
 * @returns {number|null}
 */
function parseNumber(s) {
  if (s === '' || !/^[+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?$/.test(s)) return null;
  const v = Number(s);
  if (!Number.isFinite(v)) return null;
  return v;
}
