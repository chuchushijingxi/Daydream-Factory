// 平台滚动条显隐：滚动时给滚动元素挂 sb-scrolling（main.css 据此显示滑块），
// 停止滚动约 600ms 后移除。捕获阶段监听以覆盖弹窗 Portal 与所有嵌套滚动区。
const timers = new WeakMap<Element, number>();

document.addEventListener(
  'scroll',
  (event) => {
    const target =
      event.target instanceof Element
        ? event.target
        : event.target instanceof Document
          ? event.target.documentElement
          : null;
    if (!target) return;
    target.classList.add('sb-scrolling');
    const previous = timers.get(target);
    if (previous !== undefined) window.clearTimeout(previous);
    timers.set(
      target,
      window.setTimeout(() => target.classList.remove('sb-scrolling'), 600),
    );
  },
  { capture: true, passive: true },
);
