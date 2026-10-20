import { Navigate, useLocation } from 'react-router-dom';
import { useSession } from './state/SessionContext';
import { WorkspaceProvider } from './state/WorkspaceContext';
import { MainWorkspace } from './MainWorkspace';
import { AuthPage, OperationsAuthPage } from './pages/accounts/AuthPage';
import { OperationsLayout } from './pages/operations/OperationsLayout';
import { Spinner } from './components/ui/spinner';
import { useCatalogText } from './contexts/useCatalogText';
export function Application() {
  const { c } = useCatalogText();
  const session = useSession(),
    { pathname } = useLocation();
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
            to={pathname.startsWith('/operations') ? '/operations-login' : '/login'}
            replace
          />
        )}
      </WorkspaceProvider>
    );
  if (authentication)
    return <Navigate to={operationsAuthentication ? '/operations/users' : '/projects'} replace />;
  return (
    <WorkspaceProvider
      key={
        session.epoch +
        ':' +
        (operations ? 'operations' : pathname === '/credits' ? 'credits' : 'workspace')
      }
      load={
        operations && session.user?.operationsAllowed && pathname === '/operations/models'
          ? 'operations'
          : operations || pathname === '/credits'
            ? 'none'
            : 'workspace'
      }
    >
      {operations ? <OperationsLayout /> : <MainWorkspace />}
    </WorkspaceProvider>
  );
}
