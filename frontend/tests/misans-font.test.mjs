import test from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

test('font prebuild check rejects stale, duplicate and credentialed HTML preloads offline', (t) => {
  const root = mkdtempSync(join(tmpdir(), 'misans-check-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  for (const directory of ['scripts', 'src/styles', 'public/fonts/misans']) {
    mkdirSync(join(root, directory), { recursive: true });
  }
  for (const file of ['scripts/check-misans-font.mjs', 'src/styles/misans.css']) {
    copyFileSync(new URL(`../${file}`, import.meta.url), join(root, file));
  }
  const manifest = JSON.parse(readFileSync(new URL('../public/fonts/misans-manifest.json', import.meta.url), 'utf8'));
  // Tiny transport fixtures keep these checks independent of network and cached font binaries.
  const payload = Buffer.from('wOF2fixture');
  for (const file of manifest.files) {
    file.size = payload.length;
    file.sha256 = createHash('sha256').update(payload).digest('hex');
    writeFileSync(join(root, 'public/fonts/misans', file.path), payload);
  }
  writeFileSync(join(root, 'public/fonts/misans-manifest.json'), JSON.stringify(manifest));
  const html = readFileSync(new URL('../index.html', import.meta.url), 'utf8');
  const check = (source) => {
    writeFileSync(join(root, 'index.html'), source);
    return spawnSync(process.execPath, [join(root, 'scripts/check-misans-font.mjs')], { encoding: 'utf8' });
  };
  const valid = check(html);
  assert.equal(valid.status, 0, valid.stderr);
  const preload = html.match(/<link rel="preload"[^>]+>/)[0];
  for (const invalid of [
    html.replace(preload, ''),
    html.replace(preload, preload + preload),
    html.replace(preload, preload.replace(/href="[^"]+"/, 'href="./public/fonts/misans/stale.woff2"')),
    html.replace(preload, preload.replace('crossorigin', 'crossorigin="use-credentials"')),
    html.replace(preload, preload.replace(' crossorigin', '')),
  ]) {
    const result = check(invalid);
    assert.equal(result.status, 1);
    assert.match(result.stderr, /HTML preloads must match/);
  }
});
