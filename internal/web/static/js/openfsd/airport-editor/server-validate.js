/**
 * Server validate helpers for the airport editor.
 * POST /api/v1/editor/validate-apt|validate-air with JSON {"text":"…"}.
 * Pure parsing + CSRF-aware fetch (same-origin cookie session).
 */

/**
 * @param {string|null|undefined} text
 * @returns {{ text: string }}
 */
export function buildValidateRequest(text) {
  return { text: text == null ? '' : String(text) };
}

/**
 * Read CSRF token from meta tag or openfsd_csrf cookie (double-submit).
 * @param {Document|null|undefined} [doc]
 * @returns {string}
 */
export function getCSRFToken(doc) {
  const d = doc ?? (typeof document !== 'undefined' ? document : null);
  if (!d) return '';
  const meta = d.querySelector?.('meta[name="csrf-token"]');
  if (meta && 'content' in meta && meta.content) {
    return String(meta.content);
  }
  const cookie = typeof d.cookie === 'string' ? d.cookie : '';
  const match = cookie.match(/(?:^|;\s*)openfsd_csrf=([^;]+)/);
  return match ? decodeURIComponent(match[1]) : '';
}

/**
 * Normalize APIV1 validate-apt/air response body.
 * Soft parse issues live in data.errors (HTTP 200); envelope err is for failures.
 *
 * @param {*} body
 * @param {'apt'|'air'} [side='apt']
 * @returns {{
 *   ok: boolean,
 *   error: string|null,
 *   errors: string[],
 *   summary: { icao?: string, surface_count?: number, aircraft_count?: number }
 * }}
 */
export function parseValidateAPIResponse(body, side = 'apt') {
  if (!body || typeof body !== 'object') {
    return { ok: false, error: 'invalid response', errors: [], summary: {} };
  }
  if (body.err != null && body.err !== '') {
    return { ok: false, error: String(body.err), errors: [], summary: {} };
  }
  const data = body.data && typeof body.data === 'object' ? body.data : {};
  const rawErrs = Array.isArray(data.errors) ? data.errors : [];
  const errors = rawErrs.map((e) => String(e));
  /** @type {{ icao?: string, surface_count?: number, aircraft_count?: number }} */
  const summary = {};
  if (side === 'air') {
    summary.aircraft_count = Number.isFinite(Number(data.aircraft_count))
      ? Number(data.aircraft_count)
      : 0;
  } else {
    summary.icao = data.icao != null ? String(data.icao) : '';
    summary.surface_count = Number.isFinite(Number(data.surface_count))
      ? Number(data.surface_count)
      : 0;
  }
  return { ok: true, error: null, errors, summary };
}

/**
 * POST text to a validate endpoint. credentials: 'same-origin'; CSRF on mutation.
 *
 * @param {string} url
 * @param {string} text
 * @param {{
 *   fetchImpl?: typeof fetch,
 *   csrfToken?: string,
 *   doc?: Document|null,
 *   side?: 'apt'|'air'
 * }} [opts]
 * @returns {Promise<{
 *   httpOk: boolean,
 *   status: number,
 *   ok: boolean,
 *   error: string|null,
 *   errors: string[],
 *   summary: { icao?: string, surface_count?: number, aircraft_count?: number }
 * }>}
 */
export async function postServerValidate(url, text, opts = {}) {
  const fetchImpl = opts.fetchImpl ?? (typeof fetch !== 'undefined' ? fetch : null);
  if (!fetchImpl) {
    return {
      httpOk: false,
      status: 0,
      ok: false,
      error: 'fetch unavailable',
      errors: [],
      summary: {},
    };
  }
  const side = opts.side === 'air' ? 'air' : 'apt';
  const headers = {
    Accept: 'application/json',
    'Content-Type': 'application/json',
  };
  const csrf = opts.csrfToken != null ? opts.csrfToken : getCSRFToken(opts.doc);
  if (csrf) {
    headers['X-CSRF-Token'] = csrf;
  }

  let res;
  try {
    res = await fetchImpl(url, {
      method: 'POST',
      headers,
      credentials: 'same-origin',
      body: JSON.stringify(buildValidateRequest(text)),
    });
  } catch (err) {
    const msg = err && typeof err === 'object' && 'message' in err
      ? String(/** @type {{ message: unknown }} */ (err).message)
      : 'network error';
    return {
      httpOk: false,
      status: 0,
      ok: false,
      error: msg,
      errors: [],
      summary: {},
    };
  }

  let body = null;
  const ct = res.headers?.get?.('content-type') || '';
  if (ct.includes('application/json')) {
    try {
      body = await res.json();
    } catch {
      body = null;
    }
  }

  const parsed = parseValidateAPIResponse(body, side);
  if (!res.ok) {
    const errMsg =
      parsed.error ||
      (body && body.err) ||
      res.statusText ||
      `HTTP ${res.status}`;
    return {
      httpOk: false,
      status: res.status,
      ok: false,
      error: String(errMsg),
      errors: [],
      summary: {},
    };
  }
  return {
    httpOk: true,
    status: res.status,
    ok: parsed.ok,
    error: parsed.error,
    errors: parsed.errors,
    summary: parsed.summary,
  };
}

/**
 * Aggregate client-side issue counts for badge / summary.
 * @param {{ aptErrors?: string[], airErrors?: string[], softWarnings?: string[], airport?: *, aircraft?: *[] }|null|undefined} doc
 * @returns {{ apt: number, air: number, soft: number, total: number, hasContent: boolean }}
 */
export function countClientIssues(doc) {
  const apt = Array.isArray(doc?.aptErrors) ? doc.aptErrors.length : 0;
  const air = Array.isArray(doc?.airErrors) ? doc.airErrors.length : 0;
  const soft = Array.isArray(doc?.softWarnings) ? doc.softWarnings.length : 0;
  const hasAirport = !!doc?.airport;
  const hasAircraft = Array.isArray(doc?.aircraft) && doc.aircraft.length > 0;
  return {
    apt,
    air,
    soft,
    total: apt + air + soft,
    hasContent: hasAirport || hasAircraft,
  };
}

/**
 * Titlebar / tab badge text for live client issues.
 * @param {{ total: number, hasContent: boolean, soft?: number }} counts
 * @returns {string}
 */
export function issueBadgeText(counts) {
  if (!counts) return '—';
  if (!counts.hasContent && counts.total === 0) return '—';
  if (counts.total === 0) return 'OK';
  const n = counts.total;
  const soft = counts.soft || 0;
  if (soft === n) {
    return `${n} warn${n === 1 ? '' : 's'}`;
  }
  return `${n} issue${n === 1 ? '' : 's'}`;
}

/**
 * Human summary line for the Validate tab header.
 * @param {{ apt: number, air: number, soft: number, total: number, hasContent: boolean }} counts
 * @returns {string}
 */
export function clientSummaryLine(counts) {
  if (!counts.hasContent && counts.total === 0) {
    return 'No document loaded';
  }
  if (counts.total === 0) {
    return 'Client: no parse errors or soft warnings';
  }
  const parts = [];
  if (counts.apt) parts.push(`${counts.apt} APT`);
  if (counts.air) parts.push(`${counts.air} AIR`);
  if (counts.soft) parts.push(`${counts.soft} soft`);
  return `Client: ${parts.join(' · ')} (${counts.total} total)`;
}

/**
 * Format a one-line server result summary.
 * @param {'apt'|'air'} side
 * @param {{ ok: boolean, error: string|null, errors: string[], summary: object }|null|undefined} result
 * @returns {string}
 */
export function serverResultSummary(side, result) {
  if (!result) return '';
  if (!result.ok || result.error) {
    return `Server ${side.toUpperCase()}: ${result.error || 'failed'}`;
  }
  const n = result.errors?.length || 0;
  if (side === 'air') {
    const ac = result.summary?.aircraft_count ?? 0;
    return n
      ? `Server AIR: ${n} error(s), ${ac} aircraft`
      : `Server AIR: OK (${ac} aircraft)`;
  }
  const icao = result.summary?.icao || '—';
  const sc = result.summary?.surface_count ?? 0;
  return n
    ? `Server APT: ${n} error(s), ICAO ${icao}, ${sc} surfaces`
    : `Server APT: OK (ICAO ${icao}, ${sc} surfaces)`;
}
