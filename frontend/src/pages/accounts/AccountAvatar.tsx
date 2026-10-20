import { protectedResourceUrl } from '../../api/client';
import { useState } from 'react';
import type { SessionView } from '../../api/administration';
import { useCatalogText } from '../../contexts/useCatalogText';

export function AccountAvatar({
  user,
  className = 'xy-menu-avatar',
}: {
  user: SessionView | null | undefined;
  className?: string;
}) {
  const { c } = useCatalogText();
  const [failed, setFailed] = useState<string | null>(null);
  return (
    <span className={className + ' account-avatar'} aria-hidden>
      {user?.avatarUrl && failed !== user.avatarUrl ? (
        <img
          src={protectedResourceUrl(user.avatarUrl)}
          alt=""
          referrerPolicy="no-referrer"
          onError={() => setFailed(user.avatarUrl!)}
        />
      ) : (
        Array.from(user?.username || c('剧'))[0]?.toLocaleUpperCase()
      )}
    </span>
  );
}
