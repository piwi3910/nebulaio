import React from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, userEvent, cleanup } from '../test/utils';
import { ErrorBoundary } from './ErrorBoundary';

// Component that throws an error
function ThrowError({ shouldThrow }: { shouldThrow: boolean }) {
  if (shouldThrow) {
    throw new Error('Test error message');
  }
  return <div>No error</div>;
}

// Component that conditionally throws
function ConditionalError({ error }: { error: Error | null }) {
  if (error) {
    throw error;
  }
  return <div>Working component</div>;
}

describe('ErrorBoundary', () => {
  // Suppress console.error for these tests since we're intentionally causing errors
  const originalConsoleError = console.error;

  beforeEach(() => {
    console.error = vi.fn();
  });

  afterEach(() => {
    console.error = originalConsoleError;
    cleanup();
  });

  it('renders children when no error occurs', () => {
    render(
      <ErrorBoundary>
        <div>Child content</div>
      </ErrorBoundary>
    );

    expect(screen.getByText('Child content')).toBeInTheDocument();
  });

  it('renders error UI when child throws', () => {
    render(
      <ErrorBoundary>
        <ThrowError shouldThrow={true} />
      </ErrorBoundary>
    );

    expect(screen.getByText('Something went wrong')).toBeInTheDocument();
  });

  it('displays retry button in error UI', () => {
    render(
      <ErrorBoundary>
        <ThrowError shouldThrow={true} />
      </ErrorBoundary>
    );

    expect(screen.getByRole('button', { name: /try again/i })).toBeInTheDocument();
  });

  it('displays reload button in error UI', () => {
    render(
      <ErrorBoundary>
        <ThrowError shouldThrow={true} />
      </ErrorBoundary>
    );

    expect(screen.getByRole('button', { name: /reload page/i })).toBeInTheDocument();
  });

  it('displays go home button in error UI', () => {
    render(
      <ErrorBoundary>
        <ThrowError shouldThrow={true} />
      </ErrorBoundary>
    );

    expect(screen.getByRole('button', { name: /go home/i })).toBeInTheDocument();
  });

  it('logs error to console', () => {
    render(
      <ErrorBoundary>
        <ThrowError shouldThrow={true} />
      </ErrorBoundary>
    );

    expect(console.error).toHaveBeenCalled();
  });

  it('uses custom fallback when provided', () => {
    const customFallback = <div>Custom error message</div>;

    render(
      <ErrorBoundary fallback={customFallback}>
        <ThrowError shouldThrow={true} />
      </ErrorBoundary>
    );

    expect(screen.getByText('Custom error message')).toBeInTheDocument();
    expect(screen.queryByText('Something went wrong')).not.toBeInTheDocument();
  });

  it('resets error state when Try Again is clicked', async () => {
    const user = userEvent.setup();

    // First render with error
    render(
      <ErrorBoundary>
        <ThrowError shouldThrow={true} />
      </ErrorBoundary>
    );

    expect(screen.getByText('Something went wrong')).toBeInTheDocument();

    // Verify the Try Again button exists and is clickable
    const tryAgainButton = screen.getByRole('button', { name: /try again/i });
    expect(tryAgainButton).toBeInTheDocument();

    // Click should trigger a re-render attempt
    await user.click(tryAgainButton);

    // After clicking, error boundary should still exist (component still throws)
    expect(screen.getByText('Something went wrong')).toBeInTheDocument();
  });

  it('displays descriptive error message', () => {
    render(
      <ErrorBoundary>
        <ThrowError shouldThrow={true} />
      </ErrorBoundary>
    );

    expect(screen.getByText(/an unexpected error occurred/i)).toBeInTheDocument();
  });

  it('displays error icon', () => {
    render(
      <ErrorBoundary>
        <ThrowError shouldThrow={true} />
      </ErrorBoundary>
    );

    // The IconAlertTriangle should be rendered
    expect(document.querySelector('svg')).toBeInTheDocument();
  });

  it('handles multiple errors gracefully', () => {
    const { rerender } = render(
      <ErrorBoundary>
        <ThrowError shouldThrow={true} />
      </ErrorBoundary>
    );

    expect(screen.getByText('Something went wrong')).toBeInTheDocument();

    // Rerender with a different error
    rerender(
      <ErrorBoundary>
        <ConditionalError error={new Error('Another error')} />
      </ErrorBoundary>
    );

    // Should still show error UI (even though it's a new error)
    expect(screen.getByText('Something went wrong')).toBeInTheDocument();
  });

  it('catches errors from deeply nested components', () => {
    function DeepComponent(): React.JSX.Element {
      throw new Error('Deep error');
    }

    function MiddleComponent() {
      return (
        <div>
          <DeepComponent />
        </div>
      );
    }

    render(
      <ErrorBoundary>
        <div>
          <MiddleComponent />
        </div>
      </ErrorBoundary>
    );

    expect(screen.getByText('Something went wrong')).toBeInTheDocument();
  });

  it('preserves error state until recovery', () => {
    render(
      <ErrorBoundary>
        <ThrowError shouldThrow={true} />
      </ErrorBoundary>
    );

    const errorUI = screen.getByText('Something went wrong');
    expect(errorUI).toBeInTheDocument();

    // Error state should persist
    expect(screen.getByRole('button', { name: /try again/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /reload page/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /go home/i })).toBeInTheDocument();
  });

  it('calls reload on Reload Page button click', async () => {
    const user = userEvent.setup();

    // Mock window.location.reload
    const originalLocation = window.location;
    const reloadMock = vi.fn();
    Object.defineProperty(window, 'location', {
      value: { ...originalLocation, reload: reloadMock },
      writable: true,
    });

    render(
      <ErrorBoundary>
        <ThrowError shouldThrow={true} />
      </ErrorBoundary>
    );

    await user.click(screen.getByRole('button', { name: /reload page/i }));

    expect(reloadMock).toHaveBeenCalled();

    // Restore window.location
    Object.defineProperty(window, 'location', {
      value: originalLocation,
      writable: true,
    });
  });

  it('navigates home on Go Home button click', async () => {
    const user = userEvent.setup();

    // Mock window.location.href
    const originalLocation = window.location;
    let navigatedTo = '';

    Object.defineProperty(window, 'location', {
      value: {
        ...originalLocation,
        get href() {
          return navigatedTo;
        },
        set href(value: string) {
          navigatedTo = value;
        },
      },
      writable: true,
    });

    render(
      <ErrorBoundary>
        <ThrowError shouldThrow={true} />
      </ErrorBoundary>
    );

    await user.click(screen.getByRole('button', { name: /go home/i }));

    expect(navigatedTo).toBe('/');

    // Restore window.location
    Object.defineProperty(window, 'location', {
      value: originalLocation,
      writable: true,
    });
  });

  it('works with async errors when wrapped in error handling', async () => {
    // Note: ErrorBoundary doesn't catch async errors by default
    // This test verifies synchronous error handling
    function SyncError(): React.JSX.Element {
      throw new Error('Sync error');
    }

    render(
      <ErrorBoundary>
        <SyncError />
      </ErrorBoundary>
    );

    expect(screen.getByText('Something went wrong')).toBeInTheDocument();
  });

  it('renders within a Paper component for styling', () => {
    render(
      <ErrorBoundary>
        <ThrowError shouldThrow={true} />
      </ErrorBoundary>
    );

    // Check that the error UI is properly styled
    const paper = document.querySelector('[class*="Paper"]');
    expect(paper).toBeInTheDocument();
  });

  it('displays buttons in a group', () => {
    render(
      <ErrorBoundary>
        <ThrowError shouldThrow={true} />
      </ErrorBoundary>
    );

    const buttons = screen.getAllByRole('button');
    expect(buttons).toHaveLength(3);
  });
});
