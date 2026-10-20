// 检查 UI 中尚未本地化的中文文案。
//
// 用法（在 frontend/ 下）：
//   node scripts/i18n-audit.mjs                  # 汇总所有文件
//   node scripts/i18n-audit.mjs pages/operations # 只看路径包含该片段的文件
//   node scripts/i18n-audit.mjs --detail         # 列出每处行号与原文
//   node scripts/i18n-audit.mjs --strict         # 存在未本地化文案时以非零码退出
//
// 判定规则：JSX 文本、字符串与模板字符串中的中文，若作为 c()/t()/translate()
// 的参数传入则视为已本地化。结果按启发式列出，仍需人工判断是否为运行时数据、
// 服务端原文或注释性内容。locales/ 目录不参与检查。
import ts from 'typescript';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const SRC = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../src');
const args = process.argv.slice(2);
const detailed = args.includes('--detail');
const strict = args.includes('--strict');
const filters = args.filter((arg) => !arg.startsWith('--'));
const CJK = /[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]/;
const LOCALIZE = new Set(['c', 't', 'tc', 'translate', 'i18n.t', 'formatMessage']);

function walk(dir, out = []) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const file = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(file, out);
    else if (/\.(tsx?|mts)$/.test(entry.name) && !/\.d\.ts$/.test(entry.name)) out.push(file);
  }
  return out;
}

function calleeName(node) {
  if (ts.isIdentifier(node)) return node.text;
  if (ts.isPropertyAccessExpression(node)) {
    const owner = calleeName(node.expression);
    return owner ? owner + '.' + node.name.text : null;
  }
  return null;
}

function isLocalizeArg(node) {
  let current = node;
  while (current && current.parent) {
    const parent = current.parent;
    if (ts.isCallExpression(parent) && parent.arguments.includes(current)) {
      const name = calleeName(parent.expression);
      if (name && (LOCALIZE.has(name) || /(^|\.)c$/.test(name) || /(^|\.)t$/.test(name)))
        return true;
    }
    if (
      ts.isJsxExpression(parent) ||
      ts.isConditionalExpression(parent) ||
      ts.isParenthesizedExpression(parent) ||
      ts.isBinaryExpression(parent)
    ) {
      current = parent;
      continue;
    }
    break;
  }
  return false;
}

function isNonUi(node) {
  let current = node;
  while (current && current.parent) {
    const parent = current.parent;
    if (ts.isImportDeclaration(parent) || ts.isExportDeclaration(parent)) return true;
    if (
      ts.isTypeNode(parent) ||
      ts.isEnumMember(parent) ||
      ts.isLiteralTypeNode(parent) ||
      ts.isPropertySignature(parent) ||
      ts.isImportTypeNode(parent)
    )
      return true;
    current = parent;
    if (ts.isSourceFile(current)) break;
  }
  return false;
}

const perFile = [];
for (const file of walk(SRC)) {
  const relative = path.relative(SRC, file);
  if (relative.startsWith('locales/')) continue;
  if (filters.length && !filters.some((filter) => relative.includes(filter))) continue;
  const source = fs.readFileSync(file, 'utf8');
  if (!CJK.test(source)) continue;
  const sourceFile = ts.createSourceFile(
    file,
    source,
    ts.ScriptTarget.Latest,
    true,
    file.endsWith('x') ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
  );
  const lineOf = (node) =>
    sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile)).line + 1;
  const hits = [];
  const add = (node, kind) => {
    const text = node.getText(sourceFile).replace(/\s+/g, ' ').slice(0, 140);
    if (!CJK.test(text) || isNonUi(node)) return;
    hits.push({ line: lineOf(node), kind, text });
  };
  function visit(node) {
    if (ts.isJsxText(node)) {
      const text = node.getText(sourceFile).trim();
      if (text && CJK.test(text))
        hits.push({ line: lineOf(node), kind: 'jsx-text', text: text.slice(0, 140) });
    } else if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
      if (!isLocalizeArg(node))
        add(node, node.parent && ts.isJsxAttribute(node.parent) ? 'jsx-attr' : 'string');
    } else if (ts.isTemplateExpression(node)) {
      if (!isLocalizeArg(node)) add(node, 'template');
    }
    ts.forEachChild(node, visit);
  }
  visit(sourceFile);
  if (hits.length)
    perFile.push({
      file: relative,
      hits,
      localized: /useCatalogText|useLanguage|useTranslation/.test(source),
    });
}

perFile.sort((left, right) => right.hits.length - left.hits.length);
const total = perFile.reduce((sum, entry) => sum + entry.hits.length, 0);
console.log('FILES:', perFile.length, 'HITS:', total);
for (const entry of perFile)
  console.log(
    String(entry.hits.length).padStart(4),
    entry.localized ? 'LOCALIZED' : 'NO-I18N  ',
    entry.file,
  );
if (detailed) {
  for (const entry of perFile) {
    console.log('\n## ' + entry.file);
    for (const hit of entry.hits) console.log(`  ${hit.line}\t${hit.kind}\t${hit.text}`);
  }
}
if (strict && total > 0) {
  console.error('\ni18n audit found', total, 'unlocalized Chinese string(s).');
  process.exitCode = 1;
}
