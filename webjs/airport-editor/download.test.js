import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  sanitizeFilename,
  aptDownloadFilename,
  airDownloadFilename,
  prepareAptDownload,
  prepareAirDownload,
  downloadApt,
  downloadAir,
  hashText,
  formatAptForDownload,
  formatAirForDownload,
} from '../../internal/web/static/js/openfsd/airport-editor/download.js';
import {
  createEmptyDocument,
  createEmptyAirport,
  setAirport,
  setAircraft,
  createDefaultAircraft,
} from '../../internal/web/static/js/openfsd/airport-editor/model.js';

test('sanitizeFilename', () => {
  assert.equal(sanitizeFilename('KBTV.apt', 'airport.apt'), 'KBTV.apt');
  // Path stripped to basename; basename must still match safe charset.
  assert.equal(sanitizeFilename('../evil.apt', 'airport.apt'), 'evil.apt');
  assert.equal(sanitizeFilename('path/to/x.apt', 'airport.apt'), 'x.apt');
  assert.equal(sanitizeFilename('', 'airport.apt'), 'airport.apt');
  assert.equal(sanitizeFilename('bad name.apt', 'airport.apt'), 'airport.apt');
  assert.equal(sanitizeFilename('..', 'airport.apt'), 'airport.apt');
});

test('aptDownloadFilename / airDownloadFilename', () => {
  assert.equal(aptDownloadFilename({}), 'airport.apt');
  assert.equal(
    aptDownloadFilename({ aptFilename: 'mine.apt' }),
    'mine.apt',
  );
  assert.equal(
    aptDownloadFilename({ airport: { icao: 'KBTV' } }),
    'KBTV.apt',
  );
  assert.equal(
    airDownloadFilename({ airport: { icao: 'KBTV' } }),
    'KBTV.air',
  );
  assert.equal(airDownloadFilename({ airFilename: 's.air' }), 's.air');
});

test('prepareAptDownload empty and filled', () => {
  const doc = createEmptyDocument();
  let prep = prepareAptDownload(doc);
  assert.equal(prep.empty, true);
  assert.equal(prep.text, '');

  const apt = createEmptyAirport();
  apt.icao = 'KBTV';
  setAirport(doc, apt, { markDirty: false });
  prep = prepareAptDownload(doc);
  assert.equal(prep.empty, false);
  assert.match(prep.text, /icao=KBTV/);
  assert.equal(prep.hash, hashText(prep.text));
  assert.equal(prep.filename, 'KBTV.apt');
});

test('prepareAirDownload', () => {
  const doc = createEmptyDocument();
  let prep = prepareAirDownload(doc);
  assert.equal(prep.empty, true);

  const apt = createEmptyAirport();
  apt.icao = 'KBTV';
  setAirport(doc, apt, { markDirty: false });
  setAircraft(doc, [createDefaultAircraft(apt, { lat: 1, lon: 2 })], { markDirty: false });
  prep = prepareAirDownload(doc);
  assert.equal(prep.empty, false);
  assert.ok(prep.text.includes(':'));
  assert.equal(prep.filename, 'KBTV.air');
});

test('downloadApt notes clean via initiate stub', () => {
  const doc = createEmptyDocument();
  const apt = createEmptyAirport();
  apt.icao = 'TEST';
  setAirport(doc, apt);
  assert.equal(doc.aptDirty, true);

  /** @type {Array<[string,string]>} */
  const calls = [];
  const res = downloadApt(doc, {
    initiate: (filename, text) => {
      calls.push([filename, text]);
      return true;
    },
  });
  assert.equal(res.ok, true);
  assert.equal(res.empty, false);
  assert.equal(calls.length, 1);
  assert.equal(doc.aptDirty, false);
  assert.ok(doc.lastAptDownloadHash);
});

test('downloadApt empty does not call initiate', () => {
  const doc = createEmptyDocument();
  let called = false;
  const res = downloadApt(doc, {
    initiate: () => {
      called = true;
      return true;
    },
  });
  assert.equal(res.ok, false);
  assert.equal(res.empty, true);
  assert.equal(called, false);
});

test('downloadAir marks clean', () => {
  const doc = createEmptyDocument();
  setAircraft(doc, [createDefaultAircraft(null, { lat: 0, lon: 0 })]);
  assert.equal(doc.airDirty, true);
  const res = downloadAir(doc, {
    initiate: () => true,
  });
  assert.equal(res.ok, true);
  assert.equal(doc.airDirty, false);
});

test('format helpers', () => {
  const doc = createEmptyDocument();
  assert.equal(formatAptForDownload(doc), '');
  assert.equal(formatAirForDownload(doc), '');
  const apt = createEmptyAirport();
  apt.icao = 'AAAA';
  setAirport(doc, apt, { markDirty: false });
  assert.match(formatAptForDownload(doc), /icao=AAAA/);
});
