import { Component, ErrorInfo, ReactNode } from 'react';
import { Container, Title, Text, Button, Stack, Paper, Code, Group } from '@mantine/core';
import { IconAlertTriangle, IconRefresh, IconHome } from '@tabler/icons-react';

interface ErrorBoundaryProps {
  children: ReactNode;
  fallback?: ReactNode;
}

interface ErrorBoundaryState {
  hasError: boolean;
  error: Error | null;
  errorInfo: ErrorInfo | null;
}

/**
 * ErrorBoundary catches JavaScript errors anywhere in its child component tree,
 * logs those errors, and displays a fallback UI instead of the component tree that crashed.
 */
export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = {
      hasError: false,
      error: null,
      errorInfo: null,
    };
  }

  static getDerivedStateFromError(error: Error): Partial<ErrorBoundaryState> {
    // Update state so the next render will show the fallback UI
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo): void {
    // Log the error to console for debugging
    console.error('ErrorBoundary caught an error:', error);
    console.error('Component stack:', errorInfo.componentStack);

    this.setState({ errorInfo });

    // In production, you might want to send this to an error tracking service
    // Example: Sentry.captureException(error, { extra: { componentStack: errorInfo.componentStack } });
  }

  handleReload = (): void => {
    window.location.reload();
  };

  handleGoHome = (): void => {
    window.location.href = '/';
  };

  handleRetry = (): void => {
    this.setState({ hasError: false, error: null, errorInfo: null });
  };

  render(): ReactNode {
    if (this.state.hasError) {
      // If a custom fallback is provided, use it
      if (this.props.fallback) {
        return this.props.fallback;
      }

      // Default error UI
      return (
        <Container size="sm" py="xl">
          <Paper shadow="md" p="xl" radius="md" withBorder>
            <Stack align="center" gap="lg">
              <IconAlertTriangle size={64} color="var(--mantine-color-red-6)" />

              <Title order={2} ta="center">
                Something went wrong
              </Title>

              <Text c="dimmed" ta="center" size="md">
                An unexpected error occurred. This has been logged and we'll look into it.
              </Text>

              {process.env.NODE_ENV === 'development' && this.state.error && (
                <Paper bg="gray.1" p="md" radius="sm" w="100%">
                  <Text size="sm" fw={600} mb="xs" c="red">
                    Error Details (Development Only):
                  </Text>
                  <Code block style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
                    {this.state.error.toString()}
                    {this.state.errorInfo?.componentStack && (
                      <>
                        {'\n\nComponent Stack:'}
                        {this.state.errorInfo.componentStack}
                      </>
                    )}
                  </Code>
                </Paper>
              )}

              <Group>
                <Button
                  leftSection={<IconRefresh size={18} />}
                  onClick={this.handleRetry}
                  variant="light"
                >
                  Try Again
                </Button>
                <Button
                  leftSection={<IconRefresh size={18} />}
                  onClick={this.handleReload}
                  variant="outline"
                >
                  Reload Page
                </Button>
                <Button
                  leftSection={<IconHome size={18} />}
                  onClick={this.handleGoHome}
                >
                  Go Home
                </Button>
              </Group>
            </Stack>
          </Paper>
        </Container>
      );
    }

    return this.props.children;
  }
}

export default ErrorBoundary;
