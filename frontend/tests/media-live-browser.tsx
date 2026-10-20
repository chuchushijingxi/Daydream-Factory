// Explicit isolated real-HTTP fixture. Uses no browser interception or credentials.
import React from 'react';
import { createRoot } from 'react-dom/client';
import { HashRouter } from 'react-router-dom';
import { LanguageProvider } from '../src/contexts/LanguageContext';
import { WorkspaceProvider } from '../src/state/WorkspaceContext';
import { MainWorkspace } from '../src/MainWorkspace';
import { Toaster } from '../src/components/ui/sonner';
import '../src/main.css';
import '../src/styles/page-transitions.css';
import '../src/styles/jimeng-controls.css';
import '../src/styles/asset-details.css';
import '../src/styles/asset-catalog.css';
import '../src/styles/script-upload.css';
import '../src/styles/workspace.css';
import '../src/styles/production.css';
import '../src/styles/script-parsing.css';
import '../src/styles/editor-workspace.css';
import '../src/styles/media.css';
import '../src/styles/feedback.css';
createRoot(document.getElementById('root')!).render(
  <LanguageProvider>
    <HashRouter>
      <WorkspaceProvider>
        <MainWorkspace />
        <Toaster />
        <span
          style={{
            position: 'fixed',
            bottom: 4,
            left: 68,
            fontSize: 10,
            color: '#888',
            pointerEvents: 'none',
          }}
        >
          独立测试服务 · 隔离PG/S3/NSQ · 仅免费Mock · 身份为测试夹具
        </span>
      </WorkspaceProvider>
    </HashRouter>
  </LanguageProvider>,
);
