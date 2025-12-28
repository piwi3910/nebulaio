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

// Default bucket for tests that need data
const mockBucket = {
  name: 'test-bucket',
  created_at: '2024-01-01T00:00:00Z',
  region: 'us-east-1',
  storage_class: 'STANDARD',
  versioning: 'Disabled',
};

describe('BucketsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    server.resetHandlers();
    // Default to admin user
    mockUseAuthStore.mockReturnValue({
      user: { id: '1', username: 'admin', role: 'admin' },
      accessToken: 'mock-token',
      refreshToken: 'mock-refresh-token',
      isAuthenticated: true,
      setTokens: vi.fn(),
      setUser: vi.fn(),
      logout: vi.fn(),
    } as any);

    // Default handler returns empty array
    server.use(
      http.get('/api/v1/admin/buckets', () => {
        return HttpResponse.json([]);
      })
    );
  });

  it('renders the buckets page title', () => {
    render(<BucketsPage />);
    expect(screen.getByRole('heading', { name: /buckets/i })).toBeInTheDocument();
  });

  it('shows loading state initially', () => {
    server.use(
      http.get('/api/v1/admin/buckets', async () => {
        await new Promise(resolve => setTimeout(resolve, 200));
        return HttpResponse.json([]);
      })
    );

    render(<BucketsPage />);
    expect(document.querySelector('[class*="Skeleton"]')).toBeInTheDocument();
  });

  it('shows Create Bucket button for admin users', async () => {
    render(<BucketsPage />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /create bucket/i })).toBeInTheDocument();
    });
  });

  it('hides Create Bucket button for non-admin users', async () => {
    mockUseAuthStore.mockReturnValue({
      user: { id: '2', username: 'user1', role: 'user' },
      accessToken: 'mock-token',
      refreshToken: 'mock-refresh-token',
      isAuthenticated: true,
      setTokens: vi.fn(),
      setUser: vi.fn(),
      logout: vi.fn(),
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

    render(<BucketsPage />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /create bucket/i })).toBeInTheDocument();
    });

    await user.click(screen.getByRole('button', { name: /create bucket/i }));

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });

    const nameInput = screen.getByLabelText(/bucket name/i);
    await user.type(nameInput, 'ab');
    await user.click(screen.getByRole('button', { name: /^create$/i }));

    await waitFor(() => {
      expect(screen.getByText(/at least 3 characters/i)).toBeInTheDocument();
    });
  });

  it('shows empty state when no buckets exist', async () => {
    render(<BucketsPage />);

    await waitFor(() => {
      expect(screen.getByText(/no buckets found/i)).toBeInTheDocument();
    });
  });

  it('closes create modal on cancel', async () => {
    const user = userEvent.setup();

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
