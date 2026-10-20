import { protectedResourceUrl } from '../../api/client';
import Markdown from 'react-markdown';
import { notificationImageUrl } from '../../api/notifications-client';
import { useCatalogText } from '../../contexts/useCatalogText';
// Only dedicated notification images; arbitrary private files and remote
// trackers never become browser requests. Raw HTML remains disabled.
export function notificationUrl(url: string, key: string) {
  if (key === 'src') return notificationImageUrl(url);
  if (key !== 'href') return '';
  if (url.startsWith('#')) return url;
  try {
    const value = new URL(url);
    return ['https:', 'http:'].includes(value.protocol) ? value.href : '';
  } catch {
    return '';
  }
}
export function SafeMarkdown({
  children,
  messageId,
  operatorPreview = false,
}: {
  children: string;
  messageId?: string;
  operatorPreview?: boolean;
}) {
  const { c } = useCatalogText();
  const transform = (url: string, key: string) => {
    if (key !== 'src') return notificationUrl(url, key);
    const canonical = notificationImageUrl(url, messageId);
    if (!operatorPreview) return canonical;
    return messageId && canonical
      ? canonical.replace('/api/notifications/', '/api/operations/notifications/')
      : '';
  };
  return (
    <div className="notification-markdown">
      <Markdown
        skipHtml
        urlTransform={transform}
        components={{
          a: ({ href, children }) =>
            href ? (
              <a href={href} target="_blank" rel="noopener noreferrer">
                {children}
              </a>
            ) : (
              <span>{children}</span>
            ),
          img: ({ src, alt }) =>
            src ? (
              <img src={protectedResourceUrl(src)} alt={alt || ''} referrerPolicy="no-referrer" />
            ) : (
              <span className="notification-image-alt">
                {alt ? c('[图片：{alt}]', { alt }) : c('[图片不可用]')}
              </span>
            ),
        }}
      >
        {children}
      </Markdown>
    </div>
  );
}
