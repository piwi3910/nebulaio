import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '../test/utils';
import { DashboardPage } from './DashboardPage';
import { http, HttpResponse } from 'msw';
import { server } from '../test/mocks/server';

describe('DashboardPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    server.resetHandlers();
    // Set up default handlers
    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json([]);
      }),
      http.get('/api/v1/admin/users', () => {
        return HttpResponse.json([]);
      }),
      http.get('/api/v1/admin/cluster/status', () => {
        return HttpResponse.json({ raft_state: 'Leader' });
      }),
      http.get('/api/v1/admin/storage', () => {
        return HttpResponse.json({ used: 0, total: 10000 });
      })
    );
  });

  it('renders the dashboard title', () => {
    render(<DashboardPage />);
    expect(screen.getByRole('heading', { name: /dashboard/i })).toBeInTheDocument();
  });

  it('shows loading skeletons while fetching data', () => {
    server.use(
      http.get('/api/v1/admin/buckets', async () => {
        await new Promise(resolve => setTimeout(resolve, 200));
        return HttpResponse.json([]);
      })
    );

    render(<DashboardPage />);
    expect(document.querySelector('[class*="Skeleton"]')).toBeInTheDocument();
  });

  it('displays cluster status card', async () => {
    render(<DashboardPage />);

    await waitFor(() => {
      expect(screen.getByText('Cluster Status')).toBeInTheDocument();
    });
  });

  it('displays storage overview card', async () => {
    render(<DashboardPage />);

    await waitFor(() => {
      expect(screen.getByText('Storage Overview')).toBeInTheDocument();
    });
  });

  it('displays recent buckets section', async () => {
    render(<DashboardPage />);

    await waitFor(() => {
      expect(screen.getByText('Recent Buckets')).toBeInTheDocument();
    });
  });

  it('shows empty state when no buckets exist', async () => {
    render(<DashboardPage />);

    await waitFor(() => {
      expect(screen.getByText(/no buckets created/i)).toBeInTheDocument();
    });
  });

  it('displays system health section', async () => {
    render(<DashboardPage />);

    await waitFor(() => {
      expect(screen.getByText('System Health')).toBeInTheDocument();
    });
  });

  it('handles API errors gracefully', async () => {
    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json({ error: 'Server error' }, { status: 500 });
      }),
      http.get('/api/v1/admin/users', () => {
        return HttpResponse.json({ error: 'Server error' }, { status: 500 });
      }),
      http.get('/api/v1/admin/cluster/status', () => {
        return HttpResponse.json({ error: 'Server error' }, { status: 500 });
      }),
      http.get('/api/v1/admin/storage', () => {
        return HttpResponse.json({ error: 'Server error' }, { status: 500 });
      })
    );

    render(<DashboardPage />);

    // Should still render the page structure even with errors
    expect(screen.getByRole('heading', { name: /dashboard/i })).toBeInTheDocument();
  });
});
