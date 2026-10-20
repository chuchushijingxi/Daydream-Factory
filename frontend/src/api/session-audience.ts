// Requested UI surface only. The server still authenticates its Cookie and role.
export type SessionAudience = 'workspace' | 'operations';
export function sessionAudience(): SessionAudience {
  const path = typeof window === 'undefined' ? '' : window.location.hash;
  return /^#\/operations(?:-login|\/|$)/.test(path) ? 'operations' : 'workspace';
}
export const sessionChangeChannel = (audience: SessionAudience) =>
  'drama-session-change:' + audience;
