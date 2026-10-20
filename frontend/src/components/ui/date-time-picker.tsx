import { useEffect, useId, useLayoutEffect, useRef, useState, type KeyboardEvent } from 'react';
import * as Popover from '@radix-ui/react-popover';
import { CalendarDays, ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight } from 'lucide-react';
import { Button } from './button';
import { useCatalogText, type CatalogText } from '../../contexts/useCatalogText';

const weekdays = ['日', '一', '二', '三', '四', '五', '六'];
const dayMs = 86400000;
const today = () => new Date(Date.now() + 8 * 3600000).toISOString().slice(0, 10);
const dateOf = (day: string) => new Date(day + 'T00:00:00Z');
const dayOf = (date: Date) => date.toISOString().slice(0, 10);
const moveDay = (day: string, offset: number) =>
  dayOf(new Date(dateOf(day).getTime() + offset * dayMs));
function moveMonth(day: string, offset: number) {
  const date = dateOf(day),
    number = date.getUTCDate();
  date.setUTCDate(1);
  date.setUTCMonth(date.getUTCMonth() + offset);
  const last = new Date(date.getTime());
  last.setUTCMonth(last.getUTCMonth() + 1);
  last.setUTCDate(0);
  date.setUTCDate(Math.min(number, last.getUTCDate()));
  return dayOf(date);
}
const spokenDate = (day: string, c: CatalogText) => {
  const [year, month, date] = day.split('-');
  return c('{year}年{month}月{date}日', { year, month: Number(month), date: Number(date) });
};
function validValue(value: string) {
  return (
    /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2}(\.\d{1,3})?)?$/.test(value) &&
    Number.isFinite(new Date(value + '+08:00').getTime()) &&
    dayOf(dateOf(value.slice(0, 10))) === value.slice(0, 10) &&
    Number(value.slice(11, 13)) < 24 &&
    Number(value.slice(14, 16)) < 60
  );
}

// Shanghai local value; keep saved seconds until an explicit minute edit.
// The popover is an edit draft; only Confirm/Clear changes the parent form.
export function DateTimePicker({
  label,
  value,
  onChange,
  disabled = false,
  required = false,
  min = '',
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  required?: boolean;
  min?: string;
}) {
  const { c } = useCatalogText();
  const id = useId(),
    trigger = useRef<HTMLButtonElement>(null),
    validator = useRef<HTMLInputElement>(null);
  const days = useRef(new Map<string, HTMLButtonElement>()),
    pendingFocus = useRef(false),
    outside = useRef(false);
  const [open, setOpen] = useState(false),
    [selected, setSelected] = useState(''),
    [active, setActive] = useState(today),
    [month, setMonth] = useState(() => today().slice(0, 7));
  const [hour, setHour] = useState('00'),
    [minute, setMinute] = useState('00'),
    [submitted, setSubmitted] = useState(false);
  const minimumDay = min.slice(0, 10),
    first = month + '-01';
  const gridStart = moveDay(first, -dateOf(first).getUTCDay());
  const weeks = Math.ceil(
    (dateOf(first).getUTCDay() + dateOf(moveDay(moveMonth(first, 1), -1)).getUTCDate()) / 7,
  );
  const dates = Array.from({ length: weeks * 7 }, (_, index) => moveDay(gridStart, index));
  const timeValid =
    /^\d{1,2}$/.test(hour) && Number(hour) < 24 && /^\d{1,2}$/.test(minute) && Number(minute) < 60;
  const draft = selected + 'T' + hour.padStart(2, '0') + ':' + minute.padStart(2, '0');
  const draftError = !timeValid
    ? c('时间须为00:00至23:59。')
    : selected && min && draft <= min
      ? c('结束时间须晚于开始时间。')
      : '';
  const error =
    (value &&
      (!validValue(value)
        ? c('请选择有效的日期时间。')
        : min && value <= min
          ? c('结束时间须晚于开始时间。')
          : '')) ||
    '';
  useEffect(() => {
    validator.current?.setCustomValidity(error);
  }, [error]);
  useEffect(() => {
    if (disabled) setOpen(false);
  }, [disabled]);
  useLayoutEffect(() => {
    if (pendingFocus.current) {
      pendingFocus.current = false;
      const button = days.current.get(active);
      button?.focus({ preventScroll: true });
      const area = button?.closest('.jm-date-time-days');
      if (button && area) {
        const r = button.getBoundingClientRect(),
          a = area.getBoundingClientRect();
        if (r.bottom > a.bottom) area.scrollTop += r.bottom - a.bottom;
        else if (r.top < a.top) area.scrollTop -= a.top - r.top;
      }
    }
  }, [active, month]);
  const show = (next: boolean) => {
    if (next) {
      const initial = value.slice(0, 10) || minimumDay || today();
      setSelected(value.slice(0, 10));
      setActive(initial);
      setMonth(initial.slice(0, 7));
      setHour(value.slice(11, 13) || '00');
      setMinute(value.slice(14, 16) || '00');
      outside.current = false;
    }
    setOpen(next);
  };
  const focusDay = (day: string) => {
    const next = minimumDay && day < minimumDay ? minimumDay : day;
    pendingFocus.current = true;
    setActive(next);
    setMonth(next.slice(0, 7));
  };
  const navigate = (offset: number) => {
    const next = moveMonth(first, offset);
    setMonth(next.slice(0, 7));
    setActive(minimumDay && next < minimumDay ? minimumDay : next);
  };
  const selectDay = (day: string) => {
    setSelected(day);
    setActive(day);
    setMonth(day.slice(0, 7));
  };
  const apply = () => {
    if (!selected || draftError) return;
    onChange(draft);
    setSubmitted(false);
    setOpen(false);
  };
  const keyDay = (event: KeyboardEvent<HTMLButtonElement>, day: string) => {
    const offsets: Record<string, number> = {
      ArrowLeft: -1,
      ArrowRight: 1,
      ArrowUp: -7,
      ArrowDown: 7,
      Home: -dateOf(day).getUTCDay(),
      End: 6 - dateOf(day).getUTCDay(),
    };
    if (event.key in offsets) {
      event.preventDefault();
      focusDay(moveDay(day, offsets[event.key]));
    }
    if (event.key === 'PageUp' || event.key === 'PageDown') {
      event.preventDefault();
      focusDay(moveMonth(day, (event.key === 'PageUp' ? -1 : 1) * (event.shiftKey ? 12 : 1)));
    }
  };
  const keyTime = (
    event: KeyboardEvent<HTMLInputElement>,
    raw: string,
    max: number,
    set: (value: string) => void,
  ) => {
    if (event.key === 'Enter') {
      event.preventDefault();
      apply();
    }
    if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
      event.preventDefault();
      set(
        String(
          (Number(raw || 0) + (event.key === 'ArrowUp' ? 1 : max) + max + 1) % (max + 1),
        ).padStart(2, '0'),
      );
    }
  };
  return (
    <div className="jm-date-time-field">
      <input
        ref={validator}
        className="jm-date-time-validation"
        aria-hidden
        tabIndex={-1}
        value={value}
        onChange={() => {}}
        disabled={disabled}
        required={required}
        onInvalid={(event) => {
          event.preventDefault();
          setSubmitted(true);
          trigger.current?.focus({ preventScroll: true });
        }}
      />
      <Popover.Root open={open} onOpenChange={show}>
        <Popover.Trigger asChild>
          <button
            ref={trigger}
            type="button"
            className="jm-date-time-trigger"
            aria-label={c(label)}
            aria-invalid={!!error || (submitted && !value)}
            aria-describedby={
              id + '-value' + (error || (submitted && !value) ? ' ' + id + '-error' : '')
            }
            disabled={disabled}
          >
            <span id={id + '-value'} data-placeholder={!value}>
              {value ? value.slice(0, 16).replace('T', ' ') : c('选择日期和时间')}
            </span>
            <CalendarDays aria-hidden />
          </button>
        </Popover.Trigger>
        <Popover.Portal>
          <Popover.Content
            className="jm-date-time-popover"
            align="start"
            sideOffset={6}
            collisionPadding={12}
            aria-label={c('{label}日期时间', { label: c(label) })}
            onOpenAutoFocus={(event) => {
              event.preventDefault();
              days.current.get(active)?.focus({ preventScroll: true });
            }}
            onInteractOutside={() => {
              outside.current = true;
            }}
            onEscapeKeyDown={(event) => event.stopPropagation()}
            onCloseAutoFocus={(event) => {
              event.preventDefault();
              if (!outside.current) trigger.current?.focus({ preventScroll: true });
            }}
          >
            <div className="jm-calendar-head">
              <button type="button" aria-label={c('上一年')} onClick={() => navigate(-12)}>
                <ChevronsLeft />
              </button>
              <button type="button" aria-label={c('上个月')} onClick={() => navigate(-1)}>
                <ChevronLeft />
              </button>
              <span aria-live="polite">
                {c('{year} 年 {month} 月', {
                  year: month.slice(0, 4),
                  month: Number(month.slice(5)),
                })}
              </span>
              <button type="button" aria-label={c('下个月')} onClick={() => navigate(1)}>
                <ChevronRight />
              </button>
              <button type="button" aria-label={c('下一年')} onClick={() => navigate(12)}>
                <ChevronsRight />
              </button>
            </div>
            <div className="jm-date-time-days">
              <table
                className="jm-date-time-calendar"
                role="grid"
                aria-label={c('{label}日期', { label: c(label) })}
              >
                <thead>
                  <tr>
                    {weekdays.map((day) => (
                      <th key={day} scope="col">
                        {c(day)}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {Array.from({ length: weeks }, (_, row) => (
                    <tr key={row}>
                      {dates.slice(row * 7, row * 7 + 7).map((day) => (
                        <td key={day} role="gridcell" aria-selected={selected === day}>
                          <button
                            ref={(node) => {
                              if (node) days.current.set(day, node);
                              else days.current.delete(day);
                            }}
                            type="button"
                            aria-label={spokenDate(day, c)}
                            aria-current={day === today() ? 'date' : undefined}
                            data-selected={selected === day}
                            data-outside={day.slice(0, 7) !== month}
                            disabled={!!minimumDay && day < minimumDay}
                            tabIndex={active === day ? 0 : -1}
                            onClick={() => selectDay(day)}
                            onKeyDown={(event) => keyDay(event, day)}
                          >
                            {Number(day.slice(8))}
                          </button>
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="jm-date-time-clock">
              <span>{c('时间')}</span>
              <div>
                <input
                  aria-label={c('{label}小时', { label: c(label) })}
                  inputMode="numeric"
                  maxLength={2}
                  value={hour}
                  onChange={(event) => setHour(event.target.value)}
                  onBlur={() => {
                    if (/^\d{1,2}$/.test(hour) && Number(hour) < 24) setHour(hour.padStart(2, '0'));
                  }}
                  onKeyDown={(event) => keyTime(event, hour, 23, setHour)}
                />
                <span>:</span>
                <input
                  aria-label={c('{label}分钟', { label: c(label) })}
                  inputMode="numeric"
                  maxLength={2}
                  value={minute}
                  onChange={(event) => setMinute(event.target.value)}
                  onBlur={() => {
                    if (/^\d{1,2}$/.test(minute) && Number(minute) < 60)
                      setMinute(minute.padStart(2, '0'));
                  }}
                  onKeyDown={(event) => keyTime(event, minute, 59, setMinute)}
                />
              </div>
            </div>
            {draftError ? (
              <p className="jm-date-time-error" role="alert">
                {draftError}
              </p>
            ) : null}
            <div className="jm-date-time-footer">
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  onChange('');
                  setOpen(false);
                  setSubmitted(false);
                }}
              >
                {c('清除')}
              </Button>
              <Button size="sm" disabled={!selected || !!draftError} onClick={apply}>
                {c('确定')}
              </Button>
            </div>
          </Popover.Content>
        </Popover.Portal>
      </Popover.Root>
      {error || (submitted && !value) ? (
        <p id={id + '-error'} className="jm-date-time-error" role="alert">
          {error || c('请选择日期和时间。')}
        </p>
      ) : null}
    </div>
  );
}
