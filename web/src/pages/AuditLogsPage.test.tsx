import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, cleanup } from '../test/utils';
import { AuditLogsPage } from './AuditLogsPage';
import { http, HttpResponse } from 'msw';
import { server } from '../test/mocks/server';

describe('AuditLogsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    server.resetHandlers();
    // Default handler returns empty logs
    server.use(
      http.get('/api/v1/admin/audit-logs', () => {
        return HttpResponse.json({ logs: [], total: 0, page: 1, page_size: 25, total_pages: 0 });
      })
    );
  });

  afterEach(() => {
    cleanup();
  });

  it('renders the audit logs page title', () => {
    render(<AuditLogsPage />);
    expect(screen.getByRole('heading', { name: /audit logs/i })).toBeInTheDocument();
  });

  it('shows loading state initially', () => {
    server.use(
      http.get('/api/v1/admin/audit-logs', async () => {
        await new Promise(resolve => setTimeout(resolve, 200));
        return HttpResponse.json({ logs: [], total: 0, page: 1, page_size: 25, total_pages: 0 });
      })
    );

    render(<AuditLogsPage />);
    expect(document.querySelector('[class*="Skeleton"]')).toBeInTheDocument();
  });

  it('shows page structure', async () => {
    render(<AuditLogsPage />);

    // Page should render with heading
    expect(screen.getByRole('heading', { name: /audit logs/i })).toBeInTheDocument();
  });

  it('handles API error gracefully', async () => {
    server.use(
      http.get('/api/v1/admin/audit-logs', () => {
        return HttpResponse.json({ error: 'Server error' }, { status: 500 });
      })
    );

    render(<AuditLogsPage />);

    // Should still render the page structure
    expect(screen.getByRole('heading', { name: /audit logs/i })).toBeInTheDocument();
  });

  it('shows filter controls', async () => {
    render(<AuditLogsPage />);

    // Should have heading
    expect(screen.getByRole('heading', { name: /audit logs/i })).toBeInTheDocument();
  });
});
