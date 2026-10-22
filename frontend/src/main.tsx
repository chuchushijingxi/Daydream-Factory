import './components/spaces/space-content.css';
import './styles/space-shell.css';
import './styles/team-spaces.css';
import { createRoot } from 'react-dom/client';
import { HashRouter } from 'react-router-dom';
import { Toaster } from './components/ui/sonner';
import { LanguageProvider } from './contexts/LanguageContext';
import { SessionProvider } from './state/SessionContext';
import { PlatformProvider } from './state/PlatformContext';
import { Application } from './Application';
import { ErrorBoundary } from './components/ErrorBoundary';
import './main.css';
import './styles/page-transitions.css';
import './styles/jimeng-controls.css';
import './styles/asset-details.css';
import './styles/asset-catalog.css';
import './styles/script-upload.css';
import './styles/workspace.css';
import './styles/model-config.css';
import './styles/date-time-picker.css';
import './styles/production.css';
import './styles/script-parsing.css';
import './styles/editor-workspace.css';
import './styles/administration.css';
import './styles/mail-settings.css';
import './styles/notifications.css';
import './styles/account-profile.css';
import './styles/personal-credits.css';
import './styles/team-credit-menu.css';
import './styles/media.css';
import './styles/creations.css';
import './styles/interface-controls.css';
import './styles/production-layout.css';
import './styles/feedback.css';
import './styles/platform-settings.css';
import './styles/team-layout.css';
import './styles/team-interactions.css';
import './styles/team-invite-dialog.css';
import './styles/focus-visibility.css';
import './scrollbar-reveal';
import './focus-visibility';
createRoot(document.getElementById('root')!).render(
  <LanguageProvider>
    <HashRouter>
      <ErrorBoundary>
        <SessionProvider>
          <PlatformProvider>
            <Application />
            <Toaster />
          </PlatformProvider>
        </SessionProvider>
      </ErrorBoundary>
    </HashRouter>
  </LanguageProvider>,
);
