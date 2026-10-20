// 积分统一使用品牌 Logo 的圆角四芒星，尺寸由所在控件决定。
export function CreditStar() {
  return (
    <svg className="credit-star" viewBox="0 0 24 24" width="16" height="16"
      aria-hidden="true" focusable="false" stroke="none">
      <path fill="var(--credit-accent)" d="M10.45 2.7c.57-1.6 2.53-1.6 3.1 0 1.37 3.89 3.86 6.38 7.75 7.75 1.6.57 1.6 2.53 0 3.1-3.89 1.37-6.38 3.86-7.75 7.75-.57 1.6-2.53 1.6-3.1 0-1.37-3.89-3.86-6.38-7.75-7.75-1.6-.57-1.6-2.53 0-3.1 3.89-1.37 6.38-3.86 7.75-7.75Z" />
    </svg>
  );
}
