import { withTranslation, type WithTranslation } from 'react-i18next';
import { Component, ErrorInfo, ReactNode } from 'react';
import { AlertCircle } from 'lucide-react';
import { Button } from './ui/button';
interface Props extends WithTranslation {
  children: ReactNode;
  fallback?: ReactNode;
}
interface State {
  hasError: boolean;
  error?: Error;
}
class ErrorBoundaryImpl extends Component<Props, State> {
  public state: State = {
    hasError: false,
  };
  public static getDerivedStateFromError(error: Error): State {
    return {
      hasError: true,
      error,
    };
  }
  public componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    console.error('ErrorBoundary caught an error:', error, errorInfo);
  }
  private handleReload = () => {
    window.location.reload();
  };
  public render() {
    if (this.state.hasError) {
      if (this.props.fallback) {
        return this.props.fallback;
      }
      return (
        <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-slate-50 via-slate-100 to-slate-50 p-4">
          <div className="max-w-md w-full bg-white rounded-lg shadow-xl p-8 text-center space-y-6">
            <div className="w-16 h-16 bg-red-100 rounded-full flex items-center justify-center mx-auto">
              <AlertCircle className="w-8 h-8 text-red-600" />
            </div>

            <div className="space-y-2">
              <h1 className="text-2xl font-bold text-gray-900">
                {this.props.t('catalog.应用出错')}
              </h1>
              <p className="text-gray-600">
                {this.props.t('catalog.应用程序遇到了一个错误。请尝试刷新页面。')}
              </p>
            </div>

            {this.state.error && (
              <div className="bg-gray-100 rounded-lg p-4 text-left">
                <p className="text-xs font-mono text-gray-700 break-all">
                  {this.state.error.message}
                </p>
              </div>
            )}

            <div className="flex flex-col gap-3">
              <Button onClick={this.handleReload} className="w-full">
                {this.props.t('catalog.重新读取')}
              </Button>
            </div>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}
export const ErrorBoundary = withTranslation()(ErrorBoundaryImpl);
