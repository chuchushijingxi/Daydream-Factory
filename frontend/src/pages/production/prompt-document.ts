import type { PromptPart } from '../../api/storyboard-types';
export const partLength = (p: PromptPart) => (p.type === 'text' ? p.text.length : 1);
export function compact(parts: PromptPart[]) {
  const out: PromptPart[] = [];
  for (const p of parts) {
    if (p.type === 'text') {
      if (!p.text) continue;
      const prev = out.at(-1);
      if (prev?.type === 'text') prev.text += p.text;
      else out.push({ ...p });
    } else out.push({ ...p });
  }
  return out;
}
export function replaceParts(
  parts: PromptPart[],
  start: number,
  end: number,
  insertion: PromptPart[],
) {
  let pos = 0;
  const before: PromptPart[] = [],
    after: PromptPart[] = [];
  for (const p of parts) {
    const next = pos + partLength(p);
    if (next <= start) before.push(p);
    else if (pos < start && p.type === 'text')
      before.push({ type: 'text', text: p.text.slice(0, start - pos) });
    if (pos >= end) after.push(p);
    else if (next > end && p.type === 'text')
      after.push({ type: 'text', text: p.text.slice(end - pos) });
    pos = next;
  }
  return compact([...before, ...insertion, ...after]);
}
export function documentText(
  parts: PromptPart[],
  label: (p: Extract<PromptPart, { type: 'asset' }>) => string,
) {
  return parts.map((p) => (p.type === 'text' ? p.text : '@' + label(p))).join('');
}
