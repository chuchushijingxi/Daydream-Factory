export function Spinner({ button = false }: { button?: boolean }) {
  return (
    <svg
      className={button ? 'jm-spinner jm-spinner--button' : 'jm-spinner'}
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden="true"
    >
      <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="2" opacity=".12" />
      <path d="M12 3a9 9 0 0 1 9 9" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
    </svg>
  );
}
