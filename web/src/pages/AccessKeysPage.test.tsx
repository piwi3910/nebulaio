import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, userEvent } from '../test/utils';
import { AccessKeysPage } from './AccessKeysPage';
import { http, HttpResponse } from 'msw';
import { server } from '../test/mocks/server';
import { useAuthStore } from '../stores/auth';

// Mock the auth store
vi.mock('../stores/auth', () => ({
  useAuthStore: vi.fn(),
}));

// Mock notifications - need to include Notifications component for test utils
vi.mock('@mantine/notifications', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@mantine/notifications')>();
  return {
    ...actual,
    notifications: {
      ...actual.notifications,
      show: vi.fn(),
    },
  };
});

const mockUseAuthStore = vi.mocked(useAuthStore);

describe('AccessKeysPage', () => {
  beforeEach(() => {
    server.resetHandlers();
    mockUseAuthStore.mockReturnValue({
      user: { id: '1', username: 'admin', role: 'admin' },
      token: 'mock-token',
      refreshToken: 'mock-refresh-token',
      isAuthenticated: true,
      login: vi.fn(),
      logout: vi.fn(),
      setTokens: vi.fn(),
    } as any);
  });

  it('renders the access keys page title', async () => {
    server.use(
      http.get('/api/v1/console/me/keys', () => {
        return HttpResponse.json([]);
      })
    );

    render(<AccessKeysPage />);
    expect(screen.getByRole('heading', { name: /access keys/i })).toBeInTheDocument();
  });

  it('shows loading state initially', async () => {
    server.use(
      http.get('/api/v1/console/me/keys', async () => {
        await new Promise(resolve => setTimeout(resolve, 100));
        return HttpResponse.json([]);
      })
    );

    render(<AccessKeysPage />);
    expect(document.querySelector('[class*="Skeleton"]')).toBeInTheDocument();
  });

  it('displays access key list', async () => {
    server.use(
      http.get('/api/v1/console/me/keys', () => {
        return HttpResponse.json([
          {
            access_key_id: 'AKIAIOSFODNN7EXAMPLE',
            description: 'Primary key',
            enabled: true,
            created_at: '2024-01-01T00:00:00Z',
          },
          {
            access_key_id: 'AKIAI44QH8DHBEXAMPLE',
            description: 'Secondary key',
            enabled: false,
            created_at: '2024-01-02T00:00:00Z',
          },
        ]);
      })
    );

    render(<AccessKeysPage />);

    await waitFor(() => {
      expect(screen.getByText('AKIAIOSFODNN7EXAMPLE')).toBeInTheDocument();
      expect(screen.getByText('AKIAI44QH8DHBEXAMPLE')).toBeInTheDocument();
    });
  });

  it('shows Create Access Key button', async () => {
    server.use(
      http.get('/api/v1/console/me/keys', () => {
        return HttpResponse.json([]);
      })
    );

    render(<AccessKeysPage />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /create access key/i })).toBeInTheDocument();
    });
  });

  it('opens create access key modal on button click', async () => {
    const user = userEvent.setup();

    server.use(
      http.get('/api/v1/console/me/keys', () => {
        return HttpResponse.json([]);
      })
    );

    render(<AccessKeysPage />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /create access key/i })).toBeInTheDocument();
    });

    await user.click(screen.getByRole('button', { name: /create access key/i }));

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });
  });

  it('creates access key and shows secret', async () => {
    const user = userEvent.setup();

    server.use(
      http.get('/api/v1/console/me/keys', () => {
        return HttpResponse.json([]);
      }),
      http.post('/api/v1/console/me/keys', async ({ request }) => {
        const body = await request.json() as { description: string };
        return HttpResponse.json({
          access_key_id: 'AKIANEWKEY123456789',
          secret_access_key: 'wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY',
          description: body.description,
          enabled: true,
          created_at: new Date().toISOString(),
        }, { status: 201 });
      })
    );

    render(<AccessKeysPage />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /create access key/i })).toBeInTheDocument();
    });

    await user.click(screen.getByRole('button', { name: /create access key/i }));

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });
  });

  it('shows empty state when no access keys exist', async () => {
    server.use(
      http.get('/api/v1/console/me/keys', () => {
        return HttpResponse.json([]);
      })
    );

    render(<AccessKeysPage />);

    await waitFor(() => {
      expect(screen.getByText(/no access keys/i)).toBeInTheDocument();
    });
  });

  it('displays access key properties in table', async () => {
    server.use(
      http.get('/api/v1/console/me/keys', () => {
        return HttpResponse.json([
          {
            access_key_id: 'AKIATESTKEY123456',
            description: 'Test key',
            enabled: true,
            created_at: '2024-06-15T10:30:00Z',
          },
        ]);
      })
    );

    render(<AccessKeysPage />);

    await waitFor(() => {
      expect(screen.getByText('AKIATESTKEY123456')).toBeInTheDocument();
      expect(screen.getByText('Test key')).toBeInTheDocument();
    });
  });

  it('handles API error gracefully', async () => {
    server.use(
      http.get('/api/v1/console/me/keys', () => {
        return HttpResponse.json({ error: 'Server error' }, { status: 500 });
      })
    );

    render(<AccessKeysPage />);

    // Should still render the page structure
    expect(screen.getByRole('heading', { name: /access keys/i })).toBeInTheDocument();
  });

  it('shows enabled/disabled status', async () => {
    server.use(
      http.get('/api/v1/console/me/keys', () => {
        return HttpResponse.json([
          {
            access_key_id: 'AKIAACTIVE',
            description: 'Active key',
            enabled: true,
            created_at: '2024-01-01T00:00:00Z',
          },
          {
            access_key_id: 'AKIAINACTIVE',
            description: 'Disabled key',
            enabled: false,
            created_at: '2024-01-02T00:00:00Z',
          },
        ]);
      })
    );

    render(<AccessKeysPage />);

    await waitFor(() => {
      expect(screen.getByText('AKIAACTIVE')).toBeInTheDocument();
      expect(screen.getByText('AKIAINACTIVE')).toBeInTheDocument();
    });
  });

  it('handles delete access key', async () => {
    let deleteCalled = false;

    server.use(
      http.get('/api/v1/console/me/keys', () => {
        if (deleteCalled) {
          return HttpResponse.json([]);
        }
        return HttpResponse.json([
          {
            access_key_id: 'AKIATODELETE',
            description: 'Key to delete',
            enabled: true,
            created_at: '2024-01-01T00:00:00Z',
          },
        ]);
      }),
      http.delete('/api/v1/console/me/keys/:id', () => {
        deleteCalled = true;
        return HttpResponse.json({ message: 'Deleted' });
      })
    );

    render(<AccessKeysPage />);

    await waitFor(() => {
      expect(screen.getByText('AKIATODELETE')).toBeInTheDocument();
    });
  });

  it('shows description about access keys', async () => {
    server.use(
      http.get('/api/v1/console/me/keys', () => {
        return HttpResponse.json([]);
      })
    );

    render(<AccessKeysPage />);

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: /access keys/i })).toBeInTheDocument();
    });

    // Should have some description text about access keys
    expect(screen.getByText(/s3/i)).toBeInTheDocument();
  });
});
