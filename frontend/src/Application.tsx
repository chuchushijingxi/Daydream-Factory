import { Navigate, useLocation } from 'react-router-dom';
import { useSession } from './state/SessionContext';
import { WorkspaceProvider } from './state/WorkspaceContext';
import { MainWorkspace } from './MainWorkspace';
import { AuthPage, OperationsAuthPage } from './pages/accounts/AuthPage';
import { OperationsLayout } from './pages/operations/OperationsLayout';
import { SpaceProvider, useSpace } from './state/SpaceContext';
import { Button } from './components/ui/button';
import { safeReturnTo } from './api/space-selection';
import { Spinner } from './components/ui/spinner';
import { useCatalogText } from './contexts/useCatalogText';
import { LegalDocumentPage, legalDocumentAt } from './pages/legal/LegalDocumentPage';
export function Application() {
  const { c } = useCatalogText();
  const session = useSession(),
    { pathname, search } = useLocation();
  const operations = pathname === '/operations' || pathname.startsWith('/operations/');
  const operationsAuthentication =
    pathname === '/operations-login' || pathname === '/operations/forgot-password';
  const authentication =
    operationsAuthentication || ['/login', '/register', '/forgot-password'].includes(pathname);
  const authenticationPage = operationsAuthentication ? (
    <OperationsAuthPage key={pathname} />
  ) : (
    <AuthPage key={pathname} />
  );
  // 协议与政策是公开的独立文档：不等会话、不进空间，也不挂任何工作台外壳。
  const legal = legalDocumentAt(pathname);
  if (legal) return <LegalDocumentPage document={legal} />;
  if (session.state === 'checking' && !authentication)
    return (
      <div className="op-feedback account-page" role="status">
        <Spinner />
        {c('正在确认会话…')}
      </div>
    );
  if (
    session.state !== 'authenticated' ||
    ((operations || operationsAuthentication) && !session.user?.operationsAllowed)
  )
    return (
      <WorkspaceProvider key={'public-' + session.epoch} load="none">
        {authentication ? (
          authenticationPage
        ) : (
          <Navigate
            to={
              pathname.startsWith('/operations')
                ? '/operations-login'
                : '/login?returnTo=' + encodeURIComponent(pathname + search)
            }
            replace
          />
        )}
      </WorkspaceProvider>
    );
  if (authentication)
    return (
      <Navigate
        to={
          operationsAuthentication
            ? '/operations/users'
            : safeReturnTo(new URLSearchParams(search).get('returnTo')) || '/projects'
        }
        replace
      />
    );
  if (operations)
    return (
      <WorkspaceProvider
        key={session.epoch + ':operations'}
        load={pathname === '/operations/models' ? 'operations' : 'none'}
      >
        <OperationsLayout />
      </WorkspaceProvider>
    );
  return (
    <SpaceProvider key={session.epoch + ':' + session.user?.identity}>
      <SpaceWorkspace />
    </SpaceProvider>
  );
}
function SpaceWorkspace() {
  const space = useSpace(),
    session = useSession(),
    { pathname } = useLocation(),
    { c } = useCatalogText();
  if (!space.ready)
    return (
      <div className="op-feedback account-page" role={space.error ? 'alert' : 'status'}>
        {space.error ? (
          <>
            <p>{space.error}</p>
            <Button onClick={() => void space.refresh()}>{c('重新读取空间')}</Button>
          </>
        ) : (
          <>
            <Spinner />
            {c('正在读取空间…')}
          </>
        )}
      </div>
    );
  const quiet =
    pathname === '/credits' ||
    pathname.startsWith('/payments/') ||
    pathname === '/invites' ||
    pathname.startsWith('/invite/') ||
    pathname.startsWith('/team/');
  return (
    <WorkspaceProvider
      key={session.epoch + ':' + space.epoch + ':' + (quiet ? 'settings' : 'workspace')}
      load={quiet ? 'none' : 'workspace'}
    >
      <MainWorkspace />
    </WorkspaceProvider>
  );
}
