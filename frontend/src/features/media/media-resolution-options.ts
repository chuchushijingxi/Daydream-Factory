import type { MediaProfile } from '../../api/types';

export function highestMediaResolution(resolutions: MediaProfile['resolutions']) {
  const size = (id: string) => {
    const parts = id.toLowerCase().split('x');
    if (parts.length === 2) return Number(parts[0]) * Number(parts[1]);
    return parseFloat(id) * (id.toLowerCase().endsWith('k') ? 1024 : 1);
  };
  return resolutions.reduce<string>(
    (highest, r) => (!highest || size(r.id) > size(highest) ? r.id : highest),
    '',
  );
}

// Named output tiers are comparable. Explicit width×height choices describe
// different shapes; keep those choices rather than discard an aspect ratio.
export function mediaResolutionOptions(kind: 'image' | 'video', profile: MediaProfile) {
  const resolutions = profile.resolutions;
  if (kind !== 'image' || resolutions.length <= 3) return resolutions;
  const ranked = resolutions.map((resolution) => {
    const match = /^(\d+(?:\.\d+)?)(K)?$/i.exec(resolution.id);
    return { resolution, size: match ? Number(match[1]) * (match[2] ? 1024 : 1) : NaN };
  });
  if (ranked.some((item) => !Number.isFinite(item.size) || item.size <= 0)) return resolutions;
  const visible = new Set(
    ranked
      .sort((a, b) => b.size - a.size)
      .slice(0, 3)
      .map((item) => item.resolution.id),
  );
  return resolutions.filter((item) => visible.has(item.id));
}
