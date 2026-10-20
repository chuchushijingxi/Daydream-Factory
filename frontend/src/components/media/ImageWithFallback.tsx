import { protectedResourceUrl } from '../../api/client';
import React, { useState } from 'react';
const ERROR_IMG_SRC =
  'data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iODgiIGhlaWdodD0iODgiIHhtbG5zPSJodHRwOi8vd3d3LnczLm9yZy8yMDAwL3N2ZyIgc3Ryb2tlPSIjMDAwIiBzdHJva2UtbGluZWpvaW49InJvdW5kIiBvcGFjaXR5PSIuMyIgZmlsbD0ibm9uZSIgc3Ryb2tlLXdpZHRoPSIzLjciPjxyZWN0IHg9IjE2IiB5PSIxNiIgd2lkdGg9IjU2IiBoZWlnaHQ9IjU2IiByeD0iNiIvPjxwYXRoIGQ9Im0xNiA1OCAxNi0xOCAzMiAzMiIvPjxjaXJjbGUgY3g9IjUzIiBjeT0iMzUiIHI9IjciLz48L3N2Zz4KCg==';
export type ImageWithFallbackProps = React.ImgHTMLAttributes<HTMLImageElement> & {
  /** 可视区内优先请求/解码：eager + fetchPriority high（与 loading 同时传时以此为准） */
  priority?: boolean;
};
export function ImageWithFallback(props: ImageWithFallbackProps) {
  const [didError, setDidError] = useState(false);
  React.useEffect(() => {
    setDidError(false);
  }, [props.src]);
  const handleError = () => {
    setDidError(true);
  };
  const { src, alt, style, className, priority, loading, fetchPriority, decoding, ...rest } = props;
  const resolvedLoading: React.ImgHTMLAttributes<HTMLImageElement>['loading'] = priority
    ? 'eager'
    : (loading ?? 'lazy');
  const resolvedFetchPriority = priority ? 'high' : fetchPriority;
  const resolvedDecoding: React.ImgHTMLAttributes<HTMLImageElement>['decoding'] = priority
    ? (decoding ?? 'sync')
    : (decoding ?? 'async');
  return didError ? (
    <div
      className={`inline-block bg-gray-100 text-center align-middle ${className ?? ''}`}
      style={style}
    >
      <div className="flex items-center justify-center w-full h-full">
        <img src={ERROR_IMG_SRC} alt="Error loading image" {...rest} data-original-url={src} />
      </div>
    </div>
  ) : (
    <img
      src={src ? protectedResourceUrl(src) : src}
      alt={alt}
      className={className}
      style={style}
      loading={resolvedLoading}
      decoding={resolvedDecoding}
      {...(resolvedFetchPriority != null
        ? {
            fetchPriority: resolvedFetchPriority,
          }
        : {})}
      {...rest}
      onError={handleError}
    />
  );
}
