import { AlertTriangle, RefreshCw } from 'lucide-react'
import { Component, type ErrorInfo, type ReactNode } from 'react'

import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'

interface Props {
  children: ReactNode
  fallbackTitle?: string
}

interface State {
  hasError: boolean
  error: Error | null
}

export class ErrorBoundary extends Component<Props, State> {
  public override state: State = {
    hasError: false,
    error: null,
  }

  public static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error }
  }

  public override componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    console.error('Uncaught component error:', error, errorInfo)
  }

  public override render() {
    if (this.state.hasError) {
      return (
        <div className="p-4">
          <Alert variant="danger">
            <div className="space-y-3">
              <div className="flex items-center gap-2 font-semibold">
                <AlertTriangle className="size-4" />
                <span>{this.props.fallbackTitle || 'Something went wrong displaying this section.'}</span>
              </div>
              <p className="text-xs font-mono text-destructive-foreground/80">
                {this.state.error?.message || 'An unexpected rendering error occurred.'}
              </p>
              <Button
                variant="outline"
                size="sm"
                onClick={() => this.setState({ hasError: false, error: null })}
                className="gap-1.5"
              >
                <RefreshCw className="size-3.5" />
                Try again
              </Button>
            </div>
          </Alert>
        </div>
      )
    }

    return this.props.children
  }
}
