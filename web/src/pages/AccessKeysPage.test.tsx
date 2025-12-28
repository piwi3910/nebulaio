import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, userEvent } from '../test/utils';
import { AccessKeysPage } from './AccessKeysPage';
import { http, HttpResponse } from 'msw';
import { server } from '../test/mocks/server';
import { useAuthStore, type User } from '../stores/auth';

// Mock the auth store
vi.mock('../stores/auth', () => ({
  useAuthStore: vi.fn(),
}));

const mockUseAuthStore = vi.mocked(useAuthStore);

// Helper to create a properly typed auth state mock
function createMockAuthState(user: User) {
  return {
    user,
    accessToken: 'mock-token',
    refreshToken: 'mock-refresh-token',
    isAuthenticated: true,
    setTokens: vi.fn(),
    setUser: vi.fn(),
    logout: vi.fn(),
  };
}

describe('AccessKeysPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    server.resetHandlers();
    mockUseAuthStore.mockReturnValue(
      createMockAuthState({ id: '1', username: 'admin', role: 'admin' })
    );

    // Default handler returns empty array
    server.use(
      http.get('/api/v1/console/me/keys', () => {
        return HttpResponse.json([]);
      })
    );
  });

  it('renders the access keys page title', () => {
    render(<AccessKeysPage />);
    expect(screen.getByRole('heading', { name: /access keys/i })).toBeInTheDocument();
  });

  it('shows loading state initially', () => {
    server.use(
      http.get('/api/v1/console/me/keys', async () => {
        await new Promise(resolve => setTimeout(resolve, 200));
        return HttpResponse.json([]);
      })
    );

    render(<AccessKeysPage />);
    expect(document.querySelector('[class*="Skeleton"]')).toBeInTheDocument();
  });

  it('shows Create Access Key button', async () => {
    render(<AccessKeysPage />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /create access key/i })).toBeInTheDocument();
    });
  });

  it('opens create access key modal on button click', async () => {
    const user = userEvent.setup();

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
    render(<AccessKeysPage />);

    await waitFor(() => {
      expect(screen.getByText(/no access keys/i)).toBeInTheDocument();
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

  it('shows description about access keys', async () => {
    render(<AccessKeysPage />);

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: /access keys/i })).toBeInTheDocument();
    });

    // Should have some description text about S3
    expect(screen.getByText(/s3/i)).toBeInTheDocument();
  });
});
