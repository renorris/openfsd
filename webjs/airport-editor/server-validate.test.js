import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  buildValidateRequest,
  parseValidateAPIResponse,
  getCSRFToken,
  postServerValidate,
  countClientIssues,
  issueBadgeText,
  clientSummaryLine,
  serverResultSummary,
} from '../../internal/web/static/js/openfsd/airport-editor/server-validate.js';
import { createEmptyDocument } from '../../internal/web/static/js/openfsd/airport-editor/model.js';
import { markServerValidationStale } from '../../internal/web/static/js/openfsd/airport-editor/model.js';

test('buildValidateRequest', () => {
  assert.deepEqual(buildValidateRequest('icao=KBTV\n'), { text: 'icao=KBTV\n' });
  assert.deepEqual(buildValidateRequest(null), { text: '' });
  assert.deepEqual(buildValidateRequest(undefined), { text: '' });
});

test('parseValidateAPIResponse apt success with errors', () => {
  const r = parseValidateAPIResponse(
    {
      version: 'v1',
      err: null,
      data: {
        errors: ['Parking area G1 has no waypoint defined.'],
        icao: 'KBTV',
        surface_count: 12,
      },
    },
    'apt',
  );
  assert.equal(r.ok, true);
  assert.equal(r.error, null);
  assert.equal(r.errors.length, 1);
  assert.match(r.errors[0], /G1/);
  assert.equal(r.summary.icao, 'KBTV');
  assert.equal(r.summary.surface_count, 12);
});

test('parseValidateAPIResponse air success empty errors', () => {
  const r = parseValidateAPIResponse(
    {
      version: 'v1',
      err: null,
      data: { errors: [], aircraft_count: 3 },
    },
    'air',
  );
  assert.equal(r.ok, true);
  assert.deepEqual(r.errors, []);
  assert.equal(r.summary.aircraft_count, 3);
});

test('parseValidateAPIResponse envelope error', () => {
  const r = parseValidateAPIResponse({ version: 'v1', err: 'forbidden', data: null }, 'apt');
  assert.equal(r.ok, false);
  assert.equal(r.error, 'forbidden');
  assert.deepEqual(r.errors, []);
});

test('parseValidateAPIResponse invalid body', () => {
  const r = parseValidateAPIResponse(null, 'apt');
  assert.equal(r.ok, false);
  assert.match(r.error || '', /invalid/);
});

test('getCSRFToken from meta and cookie', () => {
  const metaDoc = {
    querySelector(sel) {
      if (sel === 'meta[name="csrf-token"]') return { content: 'meta-tok' };
      return null;
    },
    cookie: '',
  };
  assert.equal(getCSRFToken(metaDoc), 'meta-tok');

  const cookieDoc = {
    querySelector() {
      return null;
    },
    cookie: 'openfsd_session=abc; openfsd_csrf=cookie%2Dtok; other=1',
  };
  assert.equal(getCSRFToken(cookieDoc), 'cookie-tok');

  assert.equal(getCSRFToken(null), '');
});

test('postServerValidate sends CSRF and same-origin credentials', async () => {
  /** @type {RequestInit|undefined} */
  let seen;
  const fetchImpl = async (url, opts) => {
    seen = opts;
    assert.equal(url, '/api/v1/editor/validate-apt');
    return {
      ok: true,
      status: 200,
      statusText: 'OK',
      headers: { get: () => 'application/json' },
      json: async () => ({
        version: 'v1',
        err: null,
        data: { errors: [], icao: 'KBTV', surface_count: 1 },
      }),
    };
  };
  const r = await postServerValidate('/api/v1/editor/validate-apt', 'icao=KBTV\n', {
    fetchImpl,
    csrfToken: 'tok123',
    side: 'apt',
  });
  assert.equal(r.httpOk, true);
  assert.equal(r.ok, true);
  assert.equal(r.summary.icao, 'KBTV');
  assert.equal(seen?.credentials, 'same-origin');
  assert.equal(seen?.method, 'POST');
  assert.equal(/** @type {Record<string,string>} */ (seen?.headers)['X-CSRF-Token'], 'tok123');
  assert.equal(
    /** @type {Record<string,string>} */ (seen?.headers)['Content-Type'],
    'application/json',
  );
  assert.equal(seen?.body, JSON.stringify({ text: 'icao=KBTV\n' }));
});

test('postServerValidate HTTP error surfaces body.err', async () => {
  const fetchImpl = async () => ({
    ok: false,
    status: 403,
    statusText: 'Forbidden',
    headers: { get: () => 'application/json' },
    json: async () => ({ version: 'v1', err: 'forbidden', data: null }),
  });
  const r = await postServerValidate('/api/v1/editor/validate-air', 'x', {
    fetchImpl,
    csrfToken: '',
    side: 'air',
  });
  assert.equal(r.httpOk, false);
  assert.equal(r.status, 403);
  assert.equal(r.ok, false);
  assert.equal(r.error, 'forbidden');
});

test('postServerValidate network failure', async () => {
  const fetchImpl = async () => {
    throw new Error('offline');
  };
  const r = await postServerValidate('/x', 't', { fetchImpl, side: 'apt' });
  assert.equal(r.httpOk, false);
  assert.equal(r.error, 'offline');
});

test('countClientIssues and issueBadgeText', () => {
  const empty = countClientIssues(createEmptyDocument());
  assert.equal(empty.total, 0);
  assert.equal(empty.hasContent, false);
  assert.equal(issueBadgeText(empty), '—');

  const ok = countClientIssues({
    airport: { icao: 'KBTV' },
    aircraft: [],
    aptErrors: [],
    airErrors: [],
    softWarnings: [],
  });
  assert.equal(ok.hasContent, true);
  assert.equal(issueBadgeText(ok), 'OK');

  const softOnly = countClientIssues({
    airport: { icao: 'KBTV' },
    aircraft: [{}],
    aptErrors: [],
    airErrors: [],
    softWarnings: ['dep mismatch'],
  });
  assert.equal(softOnly.total, 1);
  assert.equal(issueBadgeText(softOnly), '1 warn');

  const mixed = countClientIssues({
    airport: { icao: 'KBTV' },
    aptErrors: ['a', 'b'],
    airErrors: ['c'],
    softWarnings: ['d'],
  });
  assert.equal(mixed.total, 4);
  assert.equal(issueBadgeText(mixed), '4 issues');
  assert.match(clientSummaryLine(mixed), /2 APT/);
  assert.match(clientSummaryLine(mixed), /1 AIR/);
  assert.match(clientSummaryLine(mixed), /1 soft/);
});

test('serverResultSummary', () => {
  assert.equal(serverResultSummary('apt', null), '');
  assert.match(
    serverResultSummary('apt', {
      ok: true,
      error: null,
      errors: [],
      summary: { icao: 'KBTV', surface_count: 2 },
    }),
    /OK.*KBTV/,
  );
  assert.match(
    serverResultSummary('air', {
      ok: true,
      error: null,
      errors: ['bad row'],
      summary: { aircraft_count: 1 },
    }),
    /1 error/,
  );
  assert.match(
    serverResultSummary('apt', { ok: false, error: 'forbidden', errors: [], summary: {} }),
    /forbidden/,
  );
});

test('markServerValidationStale', () => {
  const doc = createEmptyDocument();
  markServerValidationStale(doc);
  assert.equal(doc.serverValidation, null);

  doc.serverValidation = {
    loading: false,
    stale: false,
    apt: { ok: true, error: null, errors: [], summary: {} },
  };
  markServerValidationStale(doc);
  assert.equal(doc.serverValidation.stale, true);

  doc.serverValidation = { loading: true, stale: false };
  markServerValidationStale(doc);
  assert.equal(doc.serverValidation.stale, false);
  assert.equal(doc.serverValidation.loading, true);
});
