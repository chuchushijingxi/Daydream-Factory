import type { ReactNode } from 'react';
import { I18nextProvider, useTranslation } from 'react-i18next';
import i18n from '../i18n';
export function LanguageProvider({ children }: { children: ReactNode }) {
  return <I18nextProvider i18n={i18n}>{children}</I18nextProvider>;
}
export function useLanguage() {
  const { t, i18n } = useTranslation();
  return {
    t,
    language: i18n.language,
    setLanguage: (lang: string) => i18n.changeLanguage(lang),
  };
}
