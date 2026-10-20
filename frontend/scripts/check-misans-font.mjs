/** Fail before Vite can emit a build with missing or altered MiSans resources. */
import { createHash } from 'node:crypto';
import { lstatSync, readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const manifestPath = fileURLToPath(new URL('../public/fonts/misans-manifest.json', import.meta.url));
const cssPath = fileURLToPath(new URL('../src/styles/misans.css', import.meta.url));
const htmlPath = fileURLToPath(new URL('../index.html', import.meta.url));
const defaultFontDirectory = fileURLToPath(new URL('../public/fonts/misans', import.meta.url));
const weights = [400, 500, 650, 700];
const sha256Pattern = /^[0-9a-f]{64}$/;

function requireValid(condition, message) {
  if (!condition) throw new Error(message);
}

function expectedCss(files) {
  return files.map((file) => `
    @font-face {
      font-family: 'MiSans';
      font-weight: ${file.weight};
      font-style: normal;
      font-display: optional;
      src: url('../../public/fonts/misans/${file.path}') format('woff2');
      unicode-range: ${file.unicodeRange};
    }
  `).join('').replace(/\s+/g, '');
}

function htmlAttribute(tag, name) {
  const match = tag.match(new RegExp(`\\s${name}(?:\\s*=\\s*(?:"([^"]*)"|'([^']*)'|([^\\s>]+)))?(?=\\s|/?>)`, 'i'));
  return match ? match[1] ?? match[2] ?? match[3] ?? '' : null;
}

try {
  const args = process.argv.slice(2);
  requireValid(args.length === 0 || (args.length === 2 && args[0] === '--font-dir'),
    'Usage: node scripts/check-misans-font.mjs [--font-dir <directory>]');
  const fontDirectory = args.length === 2 ? resolve(args[1]) : defaultFontDirectory;
  const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'));
  requireValid(manifest.schemaVersion === 1 && manifest.family === 'MiSans'
    && manifest.version === '4.003' && manifest.fontDisplay === 'optional'
    && JSON.stringify(manifest.weights) === JSON.stringify(weights)
    && sha256Pattern.test(manifest.sourceCssSha256), 'Invalid pinned MiSans Web manifest metadata');
  requireValid(Array.isArray(manifest.files) && manifest.files.length === 224,
    'The MiSans Web manifest must contain all 224 official WOFF2 slices');

  const seen = new Set();
  const counts = new Map(weights.map((weight) => [weight, 0]));
  let preloadCount = 0;
  const errors = [];
  for (const file of manifest.files) {
    requireValid(typeof file.path === 'string' && /^[A-Za-z0-9][A-Za-z0-9._-]*\.woff2$/.test(file.path)
      && !seen.has(file.path), `Unsafe or duplicate font basename: ${file.path}`);
    seen.add(file.path);
    requireValid(weights.includes(file.weight) && Number.isSafeInteger(file.size)
      && file.size >= 4 && file.size <= 2 * 1024 * 1024 && sha256Pattern.test(file.sha256),
    `Invalid font integrity metadata: ${file.path}`);
    requireValid(file.url === `https://cdn-file.hyperos.mi.com/mi-font-service/misans/${file.weight}/${file.path}`,
      `Unexpected official font source: ${file.path}`);
    requireValid(typeof file.unicodeRange === 'string'
      && /^U\+[0-9a-f]{1,6}(?:-[0-9a-f]{1,6})?(?:,\s*U\+[0-9a-f]{1,6}(?:-[0-9a-f]{1,6})?)*$/i.test(file.unicodeRange),
    `Invalid unicode range: ${file.path}`);
    const shouldPreload = [400, 500].includes(file.weight) && /^(?:cs|lt)\.[0-9a-f]+\.0\.woff2$/.test(file.path);
    requireValid(file.preload === shouldPreload, `Invalid critical-slice preload flag: ${file.path}`);
    preloadCount += Number(file.preload);
    counts.set(file.weight, counts.get(file.weight) + 1);
    try {
      const path = join(fontDirectory, file.path);
      const stat = lstatSync(path);
      requireValid(stat.isFile() && stat.size === file.size, 'missing file or unexpected size');
      const payload = readFileSync(path);
      requireValid(payload.length === file.size && payload.subarray(0, 4).toString('ascii') === 'wOF2'
        && createHash('sha256').update(payload).digest('hex') === file.sha256, 'SHA-256 or WOFF2 signature mismatch');
    } catch (error) {
      errors.push(`${file.path}: ${error.message}`);
    }
  }
  requireValid([...counts.values()].every((count) => count === 56) && preloadCount === 4,
    'MiSans must retain 56 slices per CSS weight and exactly four critical preloads');
  const css = readFileSync(cssPath, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '').replace(/\s+/g, '');
  requireValid(css === expectedCss(manifest.files),
    'MiSans CSS differs from its pinned manifest (sources, weights, unicode ranges, or optional display)');
  const html = readFileSync(htmlPath, 'utf8').replace(/<!--[\s\S]*?-->/g, '');
  const fontPreloads = [...html.matchAll(/<link\b[^>]*>/gi)].map(([tag]) => tag).filter((tag) =>
    htmlAttribute(tag, 'rel')?.toLowerCase().split(/\s+/).includes('preload')
    && (htmlAttribute(tag, 'as')?.toLowerCase() === 'font'
      || htmlAttribute(tag, 'href')?.includes('/fonts/misans/')));
  const preloadSources = manifest.files.filter((file) => file.preload)
    .map((file) => `./public/fonts/misans/${file.path}`).sort();
  requireValid(fontPreloads.every((tag) => htmlAttribute(tag, 'as') === 'font'
    && htmlAttribute(tag, 'type') === 'font/woff2'
    && ['', 'anonymous'].includes(htmlAttribute(tag, 'crossorigin')))
    && JSON.stringify(fontPreloads.map((tag) => htmlAttribute(tag, 'href')).sort()) === JSON.stringify(preloadSources),
  'MiSans HTML preloads must match the four manifest slices with font/woff2 and anonymous crossorigin');
  requireValid(errors.length === 0,
    `Missing or corrupt MiSans WOFF2 files (${errors.length}):\n${errors.slice(0, 10).join('\n')}`);
} catch (error) {
  console.error([
    `MiSans font check failed: ${error.message}`,
    '请先执行：npm --prefix frontend run font:fetch',
    '字体必须与 public/fonts/misans-manifest.json 中固定的大小及 SHA-256 一致；容器构建自动获取。',
  ].join('\n'));
  process.exitCode = 1;
}
