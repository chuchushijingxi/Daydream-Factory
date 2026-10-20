import i18n from '../i18n';
// 非组件模块（请求、领域、工具）按当前语言翻译文案；组件仍使用
// useCatalogText()，以便切换语言时跟随 React 重渲染。
// 只能在渲染或事件处理过程中调用，不能在模块顶层冻结当前语言。
export type Translate = (text: string, values?: Record<string, string | number>) => string;
export const translate: Translate = (text, values = {}) =>
  i18n.t('catalog.' + text, { defaultValue: text, ...values }) as string;
