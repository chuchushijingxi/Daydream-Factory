import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import { zh } from './locales/zh';
import { en } from './locales/en';
import { catalogZh, catalogEn } from './locales/catalog';
const storedLanguage =
  typeof window !== 'undefined' ? window.localStorage.getItem('language') : null;
const resolvedLanguage = storedLanguage === 'en' || storedLanguage === 'zh' ? storedLanguage : 'zh';
i18n.use(initReactI18next).init({
  resources: {
    en: {
      translation: {
        ...en,
        ...catalogEn,
      },
    },
    zh: {
      translation: {
        ...zh,
        ...catalogZh,
      },
    },
  },
  lng: resolvedLanguage,
  fallbackLng: 'zh',
  supportedLngs: ['en', 'zh'],
  keySeparator: false,
  // 文案 key 是中文原文，可能含冒号（如 9:16（竖屏）、host:port）；不把冒号当命名空间分隔。
  nsSeparator: false,
  interpolation: {
    prefix: '{',
    suffix: '}',
    escapeValue: false,
  },
  react: {
    useSuspense: false,
  },
});
const applyDocumentLanguage = (lng: string) => {
  if (typeof document !== 'undefined') document.documentElement.lang = lng;
};
applyDocumentLanguage(resolvedLanguage);
i18n.on('languageChanged', (lng) => {
  applyDocumentLanguage(lng);
  if (typeof window !== 'undefined') {
    window.localStorage.setItem('language', lng);
  }
});
export default i18n;
