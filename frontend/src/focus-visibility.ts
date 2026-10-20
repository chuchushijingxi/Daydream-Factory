// 弹窗/菜单恢复焦点可能沿用输入框的 focus-visible 状态；记录最近一次
// 导航或控件激活方式，普通输入与 Escape 关闭不改变触发器原来的方式。
document.documentElement.dataset.inputModality = 'pointer';
document.addEventListener(
  'pointerdown',
  () => {
    document.documentElement.dataset.inputModality = 'pointer';
  },
  true,
);
document.addEventListener(
  'keydown',
  (event) => {
    if (event.isComposing || event.keyCode === 229 || event.ctrlKey || event.altKey || event.metaKey)
      return;
    if (event.key === 'Tab') {
      document.documentElement.dataset.inputModality = 'keyboard';
      return;
    }
    const target = event.target instanceof Element ? event.target : null;
    const editor = target?.closest('input,textarea,[contenteditable],[role=textbox],[role=searchbox]');
    const editingText =
      editor && (
        editor.matches('textarea,[role=textbox],[role=searchbox]') ||
        (editor.matches('input') && !editor.matches(
          '[type=button],[type=submit],[type=reset],[type=checkbox],[type=radio],[type=range],[type=file],[type=color]',
        )) ||
        (editor.hasAttribute('contenteditable') && editor.getAttribute('contenteditable') !== 'false')
      );
    if (editingText) return;
    const navigation = [
      'Enter', ' ', 'ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight',
      'Home', 'End', 'PageUp', 'PageDown',
    ].includes(event.key);
    const typeahead = event.key.length === 1 && target?.closest(
      'select,[role=menu],[role=menubar],[role=menuitem],[role=menuitemcheckbox],[role=menuitemradio],[role=listbox],[role=option],[role=combobox],[role=tree],[role=treeitem]',
    );
    if (navigation || typeahead) document.documentElement.dataset.inputModality = 'keyboard';
  },
  true,
);
