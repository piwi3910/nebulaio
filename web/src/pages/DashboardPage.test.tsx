import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '../test/utils';
import { DashboardPage } from './DashboardPage';
import { http, HttpResponse } from 'msw';
import { server } from '../test/mocks/server';

describe('DashboardPage', () => {
  beforeEach(() => {
    // Reset handlers before each test
    server.resetHandlers();
  });

  it('renders the dashboard title', async () => {
    render(<DashboardPage />);
    expect(screen.getByRole('heading', { name: /dashboard/i })).toBeInTheDocument();
  });

  it('shows loading skeletons while fetching data', async () => {
    // Add delay to responses to see loading state
    server.use(
      http.get('/api/v1/admin/buckets', async () => {
        await new Promise(resolve => setTimeout(resolve, 100));
        return HttpResponse.json({ buckets: [] });
      })
    );

    render(<DashboardPage />);

    // Should show some form of loading indicator initially
    // Mantine uses Skeleton components
    expect(document.querySelector('[class*="Skeleton"]')).toBeInTheDocument();
  });

  it('displays bucket count after loading', async () => {
    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json({
          buckets: [
            { name: 'bucket1', created_at: '2024-01-01T00:00:00Z' },
            { name: 'bucket2', created_at: '2024-01-02T00:00:00Z' },
            { name: 'bucket3', created_at: '2024-01-03T00:00:00Z' },
          ],
        });
      }),
      http.get('/api/v1/admin/users', () => {
        return HttpResponse.json({ users: [] });
      }),
      http.get('/api/v1/admin/cluster/status', () => {
        return HttpResponse.json({ raft_state: 'Leader' });
      }),
      http.get('/api/v1/admin/storage', () => {
        return HttpResponse.json({ used: 1000, total: 10000 });
      })
    );

    render(<DashboardPage />);

    await waitFor(() => {
      expect(screen.getByText('3')).toBeInTheDocument();
    });
  });

  it('displays user count after loading', async () => {
    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json({ buckets: [] });
      }),
      http.get('/api/v1/admin/users', () => {
        return HttpResponse.json({
          users: [
            { id: '1', username: 'admin', role: 'admin' },
            { id: '2', username: 'user1', role: 'user' },
          ],
        });
      }),
      http.get('/api/v1/admin/cluster/status', () => {
        return HttpResponse.json({ raft_state: 'Leader' });
      }),
      http.get('/api/v1/admin/storage', () => {
        return HttpResponse.json({ used: 1000, total: 10000 });
      })
    );

    render(<DashboardPage />);

    await waitFor(() => {
      expect(screen.getByText('2')).toBeInTheDocument();
    });
  });

  it('displays cluster status card', async () => {
    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json({ buckets: [] });
      }),
      http.get('/api/v1/admin/users', () => {
        return HttpResponse.json({ users: [] });
      }),
      http.get('/api/v1/admin/cluster/status', () => {
        return HttpResponse.json({
          raft_state: 'Leader',
          leader_address: 'localhost:9003',
          cluster_id: 'test-cluster-123',
        });
      }),
      http.get('/api/v1/admin/storage', () => {
        return HttpResponse.json({ used: 1000, total: 10000 });
      })
    );

    render(<DashboardPage />);

    await waitFor(() => {
      expect(screen.getByText('Cluster Status')).toBeInTheDocument();
    });

    await waitFor(() => {
      expect(screen.getByText('Leader')).toBeInTheDocument();
    });
  });

  it('displays storage overview card', async () => {
    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json({ buckets: [] });
      }),
      http.get('/api/v1/admin/users', () => {
        return HttpResponse.json({ users: [] });
      }),
      http.get('/api/v1/admin/cluster/status', () => {
        return HttpResponse.json({ raft_state: 'Leader' });
      }),
      http.get('/api/v1/admin/storage', () => {
        return HttpResponse.json({ used: 3500000000, total: 10000000000 });
      })
    );

    render(<DashboardPage />);

    await waitFor(() => {
      expect(screen.getByText('Storage Overview')).toBeInTheDocument();
    });
  });

  it('displays recent buckets', async () => {
    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json({
          buckets: [
            { name: 'my-first-bucket', created_at: '2024-01-01T00:00:00Z' },
            { name: 'my-second-bucket', created_at: '2024-01-02T00:00:00Z' },
          ],
        });
      }),
      http.get('/api/v1/admin/users', () => {
        return HttpResponse.json({ users: [] });
      }),
      http.get('/api/v1/admin/cluster/status', () => {
        return HttpResponse.json({ raft_state: 'Leader' });
      }),
      http.get('/api/v1/admin/storage', () => {
        return HttpResponse.json({ used: 1000, total: 10000 });
      })
    );

    render(<DashboardPage />);

    await waitFor(() => {
      expect(screen.getByText('Recent Buckets')).toBeInTheDocument();
    });

    await waitFor(() => {
      expect(screen.getByText('my-first-bucket')).toBeInTheDocument();
      expect(screen.getByText('my-second-bucket')).toBeInTheDocument();
    });
  });

  it('shows empty state when no buckets exist', async () => {
    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json({ buckets: [] });
      }),
      http.get('/api/v1/admin/users', () => {
        return HttpResponse.json({ users: [] });
      }),
      http.get('/api/v1/admin/cluster/status', () => {
        return HttpResponse.json({ raft_state: 'Leader' });
      }),
      http.get('/api/v1/admin/storage', () => {
        return HttpResponse.json({ used: 0, total: 10000 });
      })
    );

    render(<DashboardPage />);

    await waitFor(() => {
      expect(screen.getByText(/no buckets created/i)).toBeInTheDocument();
    });
  });

  it('displays system health section', async () => {
    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json({ buckets: [] });
      }),
      http.get('/api/v1/admin/users', () => {
        return HttpResponse.json({ users: [] });
      }),
      http.get('/api/v1/admin/cluster/status', () => {
        return HttpResponse.json({ raft_state: 'Leader' });
      }),
      http.get('/api/v1/admin/storage', () => {
        return HttpResponse.json({ is_leader: true, used: 0, total: 10000 });
      })
    );

    render(<DashboardPage />);

    await waitFor(() => {
      expect(screen.getByText('System Health')).toBeInTheDocument();
    });

    await waitFor(() => {
      expect(screen.getByText('S3 API Server')).toBeInTheDocument();
      expect(screen.getByText('Admin API Server')).toBeInTheDocument();
      expect(screen.getByText('Metadata Store (Raft)')).toBeInTheDocument();
    });
  });

  it('displays nodes count from cluster status', async () => {
    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json({ buckets: [] });
      }),
      http.get('/api/v1/admin/users', () => {
        return HttpResponse.json({ users: [] });
      }),
      http.get('/api/v1/admin/cluster/status', () => {
        return HttpResponse.json({
          raft_state: 'Leader',
          nodes: [
            { id: 'node-1', address: '10.0.0.1:9003' },
            { id: 'node-2', address: '10.0.0.2:9003' },
            { id: 'node-3', address: '10.0.0.3:9003' },
          ],
        });
      }),
      http.get('/api/v1/admin/storage', () => {
        return HttpResponse.json({ used: 0, total: 10000 });
      })
    );

    render(<DashboardPage />);

    await waitFor(() => {
      expect(screen.getByText('Nodes')).toBeInTheDocument();
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
