import { useEffect, useId, useRef, useState, type CSSProperties, type KeyboardEvent } from 'react';
import * as Popover from '@radix-ui/react-popover';
import { ChevronDown, ChevronUp } from 'lucide-react';
import { useCatalogText } from '../../contexts/useCatalogText';

export function DurationParameter({
  value,
  durations,
  disabled,
  onChange,
}: {
  value: number;
  durations: number[];
  disabled: boolean;
  onChange: (seconds: number) => void;
}) {
  const { c } = useCatalogText();
  const [open, setOpen] = useState(false),
    [draft, setDraft] = useState(String(value));
  const slider = useRef<HTMLInputElement>(null),
    title = useId();
  const choices = [...durations].sort((a, b) => a - b),
    min = choices[0],
    max = choices.at(-1)!,
    index = choices.indexOf(value);
  useEffect(() => setDraft(String(value)), [value]);
  const changeOpen = (next: boolean) => {
    setOpen(next);
    setDraft(String(value));
  };
  const select = (seconds: number) => {
    setDraft(String(seconds));
    if (seconds !== value) onChange(seconds);
  };
  const step = (direction: number) =>
    select(choices[Math.max(0, Math.min(choices.length - 1, index + direction))]);
  const keyboard = (event: KeyboardEvent<HTMLInputElement>) => {
    if (['ArrowUp', 'ArrowRight', 'ArrowDown', 'ArrowLeft', 'Home', 'End'].includes(event.key)) {
      event.preventDefault();
      if (event.key === 'Home') select(min);
      else if (event.key === 'End') select(max);
      else step(event.key === 'ArrowUp' || event.key === 'ArrowRight' ? 1 : -1);
    }
  };
  const ticks =
    choices.length <= 10
      ? choices
      : Array.from({ length: 7 }, (_, i) => choices[Math.round((i * (choices.length - 1)) / 6)]);
  const percent = (seconds: number) => (max === min ? 0 : ((seconds - min) / (max - min)) * 100);
  return (
    <Popover.Root open={open && !disabled} onOpenChange={changeOpen}>
      <Popover.Trigger asChild>
        <button
          type="button"
          className="sb-choice media-parameter-trigger"
          aria-label={c('时长')}
          disabled={disabled}
          onKeyDown={(event) => {
            if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
              event.preventDefault();
              changeOpen(true);
            }
          }}
        >
          <span>{c('{seconds} 秒', { seconds: value })}</span>
          <ChevronDown size={12} />
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          className="surface-floating-panel media-duration-panel"
          align="start"
          sideOffset={6}
          collisionPadding={12}
          aria-labelledby={title}
          onOpenAutoFocus={(event) => {
            event.preventDefault();
            slider.current?.focus({ preventScroll: true });
          }}
        >
          <div id={title} className="media-duration-title">
            {c('选择视频生成时长')}
          </div>
          <div className="media-duration-controls">
            <div
              className="media-duration-scale"
              style={{ '--duration-fill': percent(value) + '%' } as CSSProperties}
            >
              <div className="media-duration-track" aria-hidden="true">
                <span />
              </div>
              <input
                ref={slider}
                type="range"
                aria-label={c('视频生成时长')}
                aria-valuetext={c('{seconds} 秒', { seconds: value })}
                min={min}
                max={max}
                step={1}
                value={value}
                disabled={disabled}
                onKeyDown={keyboard}
                onChange={(event) => {
                  const seconds = Number(event.target.value);
                  select(
                    choices.reduce((nearest, current) =>
                      Math.abs(current - seconds) < Math.abs(nearest - seconds) ? current : nearest,
                    ),
                  );
                }}
              />
              <div className="media-duration-ticks" aria-hidden="true">
                {ticks.map((seconds) => (
                  <span key={seconds} style={{ left: percent(seconds) + '%' }}>
                    {seconds}
                  </span>
                ))}
              </div>
            </div>
            <div className="media-duration-number">
              <input
                type="number"
                aria-label={c('视频生成秒数')}
                aria-invalid={draft !== '' && !choices.includes(Number(draft))}
                min={min}
                max={max}
                step={1}
                value={draft}
                disabled={disabled}
                onKeyDown={(event) => {
                  if (event.key === 'ArrowUp' || event.key === 'ArrowDown') keyboard(event);
                  if (event.key === 'Enter') {
                    event.preventDefault();
                    setDraft(String(value));
                  }
                }}
                onChange={(event) => {
                  setDraft(event.target.value);
                  const seconds = Number(event.target.value);
                  if (event.target.value !== '' && choices.includes(seconds)) select(seconds);
                }}
                onBlur={() => setDraft(String(value))}
              />
              <div className="media-duration-steppers">
                <button
                  type="button"
                  aria-label={c('增加时长')}
                  disabled={disabled || index === choices.length - 1}
                  onMouseDown={(event) => event.preventDefault()}
                  onClick={() => step(1)}
                >
                  <ChevronUp size={10} />
                </button>
                <button
                  type="button"
                  aria-label={c('减少时长')}
                  disabled={disabled || index === 0}
                  onMouseDown={(event) => event.preventDefault()}
                  onClick={() => step(-1)}
                >
                  <ChevronDown size={10} />
                </button>
              </div>
              <span aria-hidden="true">s</span>
            </div>
          </div>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
