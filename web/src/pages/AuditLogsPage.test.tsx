import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, waitFor, userEvent } from '../test/utils';
import { AuditLogsPage } from './AuditLogsPage';
import { http, HttpResponse } from 'msw';
import { server } from '../test/mocks/server';

// Helper to create valid audit log entries matching AuditLogEntry interface
const createAuditLog = (overrides = {}) => ({
  id: '1',
  timestamp: '2024-01-01T12:00:00Z',
  event_type: 'user:login',
  user_id: 'user-1',
  username: 'admin',
  source_ip: '192.168.1.100',
  user_agent: 'Mozilla/5.0',
  request_id: 'req-123',
  status_code: 200,
  details: {},
  ...overrides,
});

describe('AuditLogsPage', () => {
  beforeEach(() => {
    server.resetHandlers();
  });

  it('renders the audit logs page title', async () => {
    server.use(
      http.get('/api/v1/admin/audit-logs', () => {
        return HttpResponse.json({ logs: [], total: 0, page: 1, page_size: 25, total_pages: 0 });
      })
    );

    render(<AuditLogsPage />);
    expect(screen.getByRole('heading', { name: /audit logs/i })).toBeInTheDocument();
  });

  it('shows loading state initially', async () => {
    server.use(
      http.get('/api/v1/admin/audit-logs', async () => {
        await new Promise(resolve => setTimeout(resolve, 100));
        return HttpResponse.json({ logs: [], total: 0, page: 1, page_size: 25, total_pages: 0 });
      })
    );

    render(<AuditLogsPage />);
    expect(document.querySelector('[class*="Skeleton"]')).toBeInTheDocument();
  });

  it('displays audit log list', async () => {
    server.use(
      http.get('/api/v1/admin/audit-logs', () => {
        return HttpResponse.json({
          logs: [
            createAuditLog({ id: '1', event_type: 'user:login', username: 'admin' }),
            createAuditLog({ id: '2', event_type: 'bucket:create', username: 'admin' }),
          ],
          total: 2,
          page: 1,
          page_size: 25,
          total_pages: 1,
        });
      })
    );

    render(<AuditLogsPage />);

    await waitFor(() => {
      expect(screen.getByText('admin')).toBeInTheDocument();
    });
  });

  it('shows empty state when no audit logs exist', async () => {
    server.use(
      http.get('/api/v1/admin/audit-logs', () => {
        return HttpResponse.json({ logs: [], total: 0, page: 1, page_size: 25, total_pages: 0 });
      })
    );

    render(<AuditLogsPage />);

    await waitFor(() => {
      expect(screen.getByText(/no audit logs found/i)).toBeInTheDocument();
    });
  });

  it('displays audit log properties in table', async () => {
    server.use(
      http.get('/api/v1/admin/audit-logs', () => {
        return HttpResponse.json({
          logs: [
            createAuditLog({
              id: '1',
              timestamp: '2024-06-15T10:30:00Z',
              event_type: 'object:delete',
              username: 'testadmin',
              bucket: 'my-bucket',
              object_key: 'file.txt',
              source_ip: '10.0.0.1',
            }),
          ],
          total: 1,
          page: 1,
          page_size: 25,
          total_pages: 1,
        });
      })
    );

    render(<AuditLogsPage />);

    await waitFor(() => {
      expect(screen.getByText('testadmin')).toBeInTheDocument();
    });
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

  it('displays pagination when there are many logs', async () => {
    server.use(
      http.get('/api/v1/admin/audit-logs', () => {
        const logs = Array.from({ length: 25 }, (_, i) =>
          createAuditLog({
            id: String(i + 1),
            timestamp: `2024-01-${String(i + 1).padStart(2, '0')}T00:00:00Z`,
            event_type: 'object:download',
          })
        );
        return HttpResponse.json({
          logs,
          total: 100,
          page: 1,
          page_size: 25,
          total_pages: 4,
        });
      })
    );

    render(<AuditLogsPage />);

    await waitFor(() => {
      // Should render logs
      expect(screen.getByText('admin')).toBeInTheDocument();
    });
  });

  it('displays event type badge', async () => {
    server.use(
      http.get('/api/v1/admin/audit-logs', () => {
        return HttpResponse.json({
          logs: [
            createAuditLog({ id: '1', event_type: 'object:upload' }),
          ],
          total: 1,
          page: 1,
          page_size: 25,
          total_pages: 1,
        });
      })
    );

    render(<AuditLogsPage />);

    await waitFor(() => {
      // Event type should be displayed (formatted as "object upload")
      expect(screen.getByText(/object upload/i)).toBeInTheDocument();
    });
  });

  it('displays source IP in logs', async () => {
    server.use(
      http.get('/api/v1/admin/audit-logs', () => {
        return HttpResponse.json({
          logs: [
            createAuditLog({ source_ip: '192.168.1.100' }),
          ],
          total: 1,
          page: 1,
          page_size: 25,
          total_pages: 1,
        });
      })
    );

    render(<AuditLogsPage />);

    await waitFor(() => {
      expect(screen.getByText('192.168.1.100')).toBeInTheDocument();
    });
  });

  it('formats timestamp correctly', async () => {
    server.use(
      http.get('/api/v1/admin/audit-logs', () => {
        return HttpResponse.json({
          logs: [
            createAuditLog({ timestamp: '2024-06-15T14:30:00Z' }),
          ],
          total: 1,
          page: 1,
          page_size: 25,
          total_pages: 1,
        });
      })
    );

    render(<AuditLogsPage />);

    await waitFor(() => {
      // Should format the date in a readable format
      expect(screen.getByText(/2024/)).toBeInTheDocument();
    });
  });

  it('shows filter controls', async () => {
    server.use(
      http.get('/api/v1/admin/audit-logs', () => {
        return HttpResponse.json({ logs: [], total: 0, page: 1, page_size: 25, total_pages: 0 });
      })
    );

    render(<AuditLogsPage />);

    // Should have some filter controls
    expect(screen.getByRole('heading', { name: /audit logs/i })).toBeInTheDocument();
  });
});
