/**
 * Blob download helpers for the airport editor.
 * Pure path is Node-testable; DOM download is behind initiateBlobDownload.
 *
 * Security: client-only Blob + temporary <a download> — never POSTs file
 * content to the server for the JS save path.
 */

import { formatAPT } from './format-apt.js';
import { formatAIR } from './format-air.js';
import { hashText, noteDownload } from './model.js';

/**
 * Sanitize download filename (basename only, safe charset).
 * @param {string|null|undefined} name
 * @param {string} fallback
 * @returns {string}
 */
export function sanitizeFilename(name, fallback) {
  const base = String(name || '').split(/[/\\]/).pop() || '';
  // Reject empty, dotted-only (., ..), and anything outside safe charset.
  if (!base || base === '.' || base === '..') return fallback;
  if (/^[A-Za-z0-9._\-]{1,64}$/.test(base)) return base;
  return fallback;
}

/**
 * Default APT download filename from document.
 * @param {{ aptFilename?: string|null, airport?: { icao?: string }|null }} doc
 * @returns {string}
 */
export function aptDownloadFilename(doc) {
  if (doc.aptFilename) return sanitizeFilename(doc.aptFilename, 'airport.apt');
  const icao = String(doc.airport?.icao || '').toUpperCase();
  if (/^[A-Z0-9]{3,4}$/.test(icao)) return `${icao}.apt`;
  return 'airport.apt';
}

/**
 * Default AIR download filename from document.
 * @param {{ airFilename?: string|null, airport?: { icao?: string }|null }} doc
 * @returns {string}
 */
export function airDownloadFilename(doc) {
  if (doc.airFilename) return sanitizeFilename(doc.airFilename, 'scenario.air');
  const icao = String(doc.airport?.icao || '').toUpperCase();
  if (/^[A-Z0-9]{3,4}$/.test(icao)) return `${icao}.air`;
  return 'scenario.air';
}

/**
 * Format APT text for download (empty airport → empty string).
 * @param {import('./model.js').EditorDocument} doc
 * @returns {string}
 */
export function formatAptForDownload(doc) {
  if (!doc.airport) return '';
  return formatAPT(doc.airport);
}

/**
 * Format AIR text for download.
 * @param {import('./model.js').EditorDocument} doc
 * @returns {string}
 */
export function formatAirForDownload(doc) {
  if (!doc.aircraft?.length) return '';
  return formatAIR(doc.aircraft);
}

/**
 * Prepare APT download payload (text + filename + hash).
 * @param {import('./model.js').EditorDocument} doc
 * @returns {{ text: string, filename: string, hash: string, empty: boolean }}
 */
export function prepareAptDownload(doc) {
  const text = formatAptForDownload(doc);
  return {
    text,
    filename: aptDownloadFilename(doc),
    hash: hashText(text),
    empty: !text,
  };
}

/**
 * Prepare AIR download payload.
 * @param {import('./model.js').EditorDocument} doc
 * @returns {{ text: string, filename: string, hash: string, empty: boolean }}
 */
export function prepareAirDownload(doc) {
  const text = formatAirForDownload(doc);
  return {
    text,
    filename: airDownloadFilename(doc),
    hash: hashText(text),
    empty: !text,
  };
}

/**
 * Initiate browser Blob download via temporary <a download>.
 * No-op in non-DOM environments (returns false).
 *
 * @param {string} filename
 * @param {string} text
 * @param {string} [mime='text/plain;charset=utf-8']
 * @param {{ createObjectURL?: Function, revokeObjectURL?: Function, document?: Document }} [deps]
 * @returns {boolean} true if click was dispatched
 */
export function initiateBlobDownload(filename, text, mime = 'text/plain;charset=utf-8', deps = {}) {
  const doc = deps.document || (typeof document !== 'undefined' ? document : null);
  const createURL =
    deps.createObjectURL ||
    (typeof URL !== 'undefined' && URL.createObjectURL
      ? URL.createObjectURL.bind(URL)
      : null);
  const revokeURL =
    deps.revokeObjectURL ||
    (typeof URL !== 'undefined' && URL.revokeObjectURL
      ? URL.revokeObjectURL.bind(URL)
      : null);

  if (!doc || !createURL || typeof Blob === 'undefined') return false;

  const blob = new Blob([text], { type: mime });
  const url = createURL(blob);
  const a = doc.createElement('a');
  a.href = url;
  a.download = filename;
  a.rel = 'noopener';
  a.style.display = 'none';
  doc.body.appendChild(a);
  a.click();
  a.remove();
  // Revoke after a tick so the browser can start the download.
  if (revokeURL) {
    setTimeout(() => {
      try {
        revokeURL(url);
      } catch {
        /* ignore */
      }
    }, 1000);
  }
  return true;
}

/**
 * Download APT via Blob and mark clean with hash (when download starts).
 * @param {import('./model.js').EditorDocument} doc
 * @param {{ initiate?: typeof initiateBlobDownload }} [opts]
 * @returns {{ ok: boolean, empty: boolean, filename: string, text: string }}
 */
export function downloadApt(doc, opts = {}) {
  const prep = prepareAptDownload(doc);
  const init = opts.initiate || initiateBlobDownload;
  if (prep.empty) {
    return { ok: false, empty: true, filename: prep.filename, text: prep.text };
  }
  const started = init(prep.filename, prep.text);
  if (started) noteDownload(doc, 'apt', prep.text);
  return { ok: started, empty: false, filename: prep.filename, text: prep.text };
}

/**
 * Download AIR via Blob and mark clean with hash.
 * @param {import('./model.js').EditorDocument} doc
 * @param {{ initiate?: typeof initiateBlobDownload }} [opts]
 * @returns {{ ok: boolean, empty: boolean, filename: string, text: string }}
 */
export function downloadAir(doc, opts = {}) {
  const prep = prepareAirDownload(doc);
  const init = opts.initiate || initiateBlobDownload;
  if (prep.empty) {
    return { ok: false, empty: true, filename: prep.filename, text: prep.text };
  }
  const started = init(prep.filename, prep.text);
  if (started) noteDownload(doc, 'air', prep.text);
  return { ok: started, empty: false, filename: prep.filename, text: prep.text };
}

export { hashText };
