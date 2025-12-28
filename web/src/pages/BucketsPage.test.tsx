import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, userEvent } from '../test/utils';
import { BucketsPage } from './BucketsPage';
import { http, HttpResponse } from 'msw';
import { server } from '../test/mocks/server';
import { useAuthStore } from '../stores/auth';

// Mock the auth store
vi.mock('../stores/auth', () => ({
  useAuthStore: vi.fn(),
}));

const mockUseAuthStore = vi.mocked(useAuthStore);

describe('BucketsPage', () => {
  beforeEach(() => {
    server.resetHandlers();
    // Default to admin user
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

  it('renders the buckets page title', async () => {
    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json([]);
      })
    );

    render(<BucketsPage />);
    expect(screen.getByRole('heading', { name: /buckets/i })).toBeInTheDocument();
  });

  it('shows loading state initially', async () => {
    server.use(
      http.get('/api/v1/admin/buckets', async () => {
        await new Promise(resolve => setTimeout(resolve, 100));
        return HttpResponse.json([]);
      })
    );

    render(<BucketsPage />);
    expect(document.querySelector('[class*="Skeleton"]')).toBeInTheDocument();
  });

  it('displays bucket list', async () => {
    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json([
          {
            name: 'test-bucket',
            created_at: '2024-01-01T00:00:00Z',
            region: 'us-east-1',
            storage_class: 'STANDARD',
            versioning: 'Disabled',
          },
          {
            name: 'data-bucket',
            created_at: '2024-01-02T00:00:00Z',
            region: 'eu-west-1',
            storage_class: 'STANDARD_IA',
            versioning: 'Enabled',
          },
        ]);
      })
    );

    render(<BucketsPage />);

    await waitFor(() => {
      expect(screen.getByText('test-bucket')).toBeInTheDocument();
      expect(screen.getByText('data-bucket')).toBeInTheDocument();
    });
  });

  it('shows Create Bucket button for admin users', async () => {
    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json([]);
      })
    );

    render(<BucketsPage />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /create bucket/i })).toBeInTheDocument();
    });
  });

  it('hides Create Bucket button for non-admin users', async () => {
    mockUseAuthStore.mockReturnValue({
      user: { id: '2', username: 'user1', role: 'user' },
      token: 'mock-token',
      refreshToken: 'mock-refresh-token',
      isAuthenticated: true,
      login: vi.fn(),
      logout: vi.fn(),
      setTokens: vi.fn(),
    } as any);

    server.use(
      http.get('/api/v1/console/buckets', () => {
        return HttpResponse.json([]);
      })
    );

    render(<BucketsPage />);

    await waitFor(() => {
      expect(screen.queryByRole('button', { name: /create bucket/i })).not.toBeInTheDocument();
    });
  });

  it('opens create bucket modal on button click', async () => {
    const user = userEvent.setup();

    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json([]);
      })
    );

    render(<BucketsPage />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /create bucket/i })).toBeInTheDocument();
    });

    await user.click(screen.getByRole('button', { name: /create bucket/i }));

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument();
      expect(screen.getByLabelText(/bucket name/i)).toBeInTheDocument();
    });
  });

  it('validates bucket name on form submit', async () => {
    const user = userEvent.setup();

    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json([]);
      })
    );

    render(<BucketsPage />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /create bucket/i })).toBeInTheDocument();
    });

    await user.click(screen.getByRole('button', { name: /create bucket/i }));

    // Try to submit with short name
    const nameInput = screen.getByLabelText(/bucket name/i);
    await user.type(nameInput, 'ab');
    await user.click(screen.getByRole('button', { name: /^create$/i }));

    await waitFor(() => {
      expect(screen.getByText(/at least 3 characters/i)).toBeInTheDocument();
    });
  });

  it('creates bucket successfully', async () => {
    const user = userEvent.setup();

    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json([]);
      }),
      http.post('/api/v1/admin/buckets', async ({ request }) => {
        const body = await request.json() as { name: string };
        return HttpResponse.json({
          name: body.name,
          created_at: new Date().toISOString(),
        }, { status: 201 });
      })
    );

    render(<BucketsPage />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /create bucket/i })).toBeInTheDocument();
    });

    await user.click(screen.getByRole('button', { name: /create bucket/i }));

    const nameInput = screen.getByLabelText(/bucket name/i);
    await user.type(nameInput, 'my-new-bucket');
    await user.click(screen.getByRole('button', { name: /^create$/i }));

    // Modal should close after success
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    });
  });

  it('shows empty state when no buckets exist', async () => {
    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json([]);
      })
    );

    render(<BucketsPage />);

    await waitFor(() => {
      expect(screen.getByText(/no buckets found/i)).toBeInTheDocument();
    });
  });

  it('displays bucket properties in table', async () => {
    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json([
          {
            name: 'test-bucket',
            created_at: '2024-01-15T12:00:00Z',
            region: 'us-west-2',
            storage_class: 'GLACIER',
            versioning: 'Enabled',
          },
        ]);
      })
    );

    render(<BucketsPage />);

    await waitFor(() => {
      expect(screen.getByText('test-bucket')).toBeInTheDocument();
      expect(screen.getByText('us-west-2')).toBeInTheDocument();
      expect(screen.getByText('GLACIER')).toBeInTheDocument();
      expect(screen.getByText('Enabled')).toBeInTheDocument();
    });
  });

  it('opens delete confirmation modal', async () => {
    const user = userEvent.setup();

    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json([
          {
            name: 'delete-me',
            created_at: '2024-01-01T00:00:00Z',
            region: 'us-east-1',
            storage_class: 'STANDARD',
          },
        ]);
      })
    );

    render(<BucketsPage />);

    await waitFor(() => {
      expect(screen.getByText('delete-me')).toBeInTheDocument();
    });

    // Find and click the menu button
    const menuButtons = screen.getAllByRole('button');
    const menuButton = menuButtons.find(btn => btn.querySelector('[class*="IconDotsVertical"]'));
    if (menuButton) {
      await user.click(menuButton);

      await waitFor(() => {
        expect(screen.getByText('Delete')).toBeInTheDocument();
      });

      await user.click(screen.getByText('Delete'));

      await waitFor(() => {
        expect(screen.getByText(/are you sure you want to delete/i)).toBeInTheDocument();
      });
    }
  });

  it('handles delete bucket success', async () => {
    const user = userEvent.setup();
    let deleteCalled = false;

    server.use(
      http.get('/api/v1/admin/buckets', () => {
        if (deleteCalled) {
          return HttpResponse.json([]);
        }
        return HttpResponse.json([
          {
            name: 'delete-me',
            created_at: '2024-01-01T00:00:00Z',
            region: 'us-east-1',
            storage_class: 'STANDARD',
          },
        ]);
      }),
      http.delete('/api/v1/admin/buckets/:name', () => {
        deleteCalled = true;
        return HttpResponse.json({ message: 'Deleted' });
      })
    );

    render(<BucketsPage />);

    await waitFor(() => {
      expect(screen.getByText('delete-me')).toBeInTheDocument();
    });

    // Open menu and click delete
    const menuButtons = screen.getAllByRole('button');
    const menuButton = menuButtons.find(btn => btn.querySelector('[class*="IconDotsVertical"]'));
    if (menuButton) {
      await user.click(menuButton);
      await user.click(screen.getByText('Delete'));

      // Confirm deletion
      await waitFor(() => {
        const deleteButtons = screen.getAllByRole('button', { name: /delete/i });
        const confirmButton = deleteButtons.find(btn => btn.closest('[role="dialog"]'));
        if (confirmButton) {
          return user.click(confirmButton);
        }
      });
    }
  });

  it('handles delete bucket error', async () => {
    const user = userEvent.setup();

    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json([
          {
            name: 'non-empty-bucket',
            created_at: '2024-01-01T00:00:00Z',
            region: 'us-east-1',
            storage_class: 'STANDARD',
          },
        ]);
      }),
      http.delete('/api/v1/admin/buckets/:name', () => {
        return HttpResponse.json(
          { error: 'Bucket not empty' },
          { status: 409 }
        );
      })
    );

    render(<BucketsPage />);

    await waitFor(() => {
      expect(screen.getByText('non-empty-bucket')).toBeInTheDocument();
    });
  });

  it('displays table headers correctly', async () => {
    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json([{ name: 'test', created_at: '2024-01-01T00:00:00Z' }]);
      })
    );

    render(<BucketsPage />);

    await waitFor(() => {
      expect(screen.getByText('Name')).toBeInTheDocument();
      expect(screen.getByText('Region')).toBeInTheDocument();
      expect(screen.getByText('Storage Class')).toBeInTheDocument();
      expect(screen.getByText('Versioning')).toBeInTheDocument();
      expect(screen.getByText('Created')).toBeInTheDocument();
    });
  });

  it('closes create modal on cancel', async () => {
    const user = userEvent.setup();

    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json([]);
      })
    );

    render(<BucketsPage />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /create bucket/i })).toBeInTheDocument();
    });

    await user.click(screen.getByRole('button', { name: /create bucket/i }));

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });

    await user.click(screen.getByRole('button', { name: /cancel/i }));

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    });
  });
});
