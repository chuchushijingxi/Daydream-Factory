export function ViewTabs({
  items,
  value,
  onChange,
  label,
  prefix,
  variant = 'line',
  panelId,
}: {
  items: { value: string; label: string }[];
  value: string;
  onChange: (value: string) => void;
  label: string;
  prefix: string;
  variant?: 'line' | 'pill';
  panelId?: string;
}) {
  return (
    <div role="tablist" aria-label={label} className="view-tabs" data-variant={variant}>
      {items.map((item, index) => (
        <button
          type="button"
          role="tab"
          id={prefix + item.value}
          key={item.value}
          aria-selected={value === item.value}
          aria-controls={panelId}
          tabIndex={value === item.value ? 0 : -1}
          onClick={() => onChange(item.value)}
          onKeyDown={(event) => {
            if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
            event.preventDefault();
            const next =
              event.key === 'Home'
                ? 0
                : event.key === 'End'
                  ? items.length - 1
                  : (index + (event.key === 'ArrowRight' ? 1 : items.length - 1)) % items.length;
            onChange(items[next].value);
            document.getElementById(prefix + items[next].value)?.focus({ preventScroll: true });
          }}
        >
          {item.label}
        </button>
      ))}
    </div>
  );
}
