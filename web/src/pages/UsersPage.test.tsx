import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, userEvent } from '../test/utils';
import { UsersPage } from './UsersPage';
import { http, HttpResponse } from 'msw';
import { server } from '../test/mocks/server';

describe('UsersPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    server.resetHandlers();
    // Default handler returns empty array
    server.use(
      http.get('/api/v1/admin/users', () => {
        return HttpResponse.json([]);
      })
    );
  });

  it('renders the users page title', () => {
    render(<UsersPage />);
    expect(screen.getByRole('heading', { name: /users/i })).toBeInTheDocument();
  });

  it('shows loading state initially', () => {
    server.use(
      http.get('/api/v1/admin/users', async () => {
        await new Promise(resolve => setTimeout(resolve, 200));
        return HttpResponse.json([]);
      })
    );

    render(<UsersPage />);
    expect(document.querySelector('[class*="Skeleton"]')).toBeInTheDocument();
  });

  it('shows Create User button', async () => {
    render(<UsersPage />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /create user/i })).toBeInTheDocument();
    });
  });

  it('opens create user modal on button click', async () => {
    const user = userEvent.setup();

    render(<UsersPage />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /create user/i })).toBeInTheDocument();
    });

    await user.click(screen.getByRole('button', { name: /create user/i }));

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument();
      expect(screen.getByLabelText(/username/i)).toBeInTheDocument();
      expect(screen.getByLabelText(/password/i)).toBeInTheDocument();
    });
  });

  it('shows empty state when no users exist', async () => {
    render(<UsersPage />);

    await waitFor(() => {
      expect(screen.getByText(/no users found/i)).toBeInTheDocument();
    });
  });

  it('handles API error gracefully', async () => {
    server.use(
      http.get('/api/v1/admin/users', () => {
        return HttpResponse.json({ error: 'Server error' }, { status: 500 });
      })
    );

    render(<UsersPage />);

    // Should still render the page structure
    expect(screen.getByRole('heading', { name: /users/i })).toBeInTheDocument();
  });

  it('validates required fields', async () => {
    const user = userEvent.setup();

    render(<UsersPage />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /create user/i })).toBeInTheDocument();
    });

    await user.click(screen.getByRole('button', { name: /create user/i }));

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });

    // Try to submit empty form - modal should stay open
    const createButtons = screen.getAllByRole('button', { name: /create/i });
    const submitButton = createButtons.find(btn => btn.closest('form') || btn.closest('[role="dialog"]'));
    if (submitButton) {
      await user.click(submitButton);
    }

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });
  });

  it('closes modal on cancel', async () => {
    const user = userEvent.setup();

    render(<UsersPage />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /create user/i })).toBeInTheDocument();
    });

    await user.click(screen.getByRole('button', { name: /create user/i }));

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });

    const cancelButton = screen.getByRole('button', { name: /cancel/i });
    await user.click(cancelButton);

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    });
  });
});
