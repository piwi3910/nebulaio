import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, userEvent, cleanup } from '../test/utils';
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

  afterEach(() => {
    cleanup();
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

  it('prevents form submission when fields are empty', async () => {
    const user = userEvent.setup();

    // Set up handler to track if API is called
    let apiWasCalled = false;
    server.use(
      http.post('/api/v1/admin/users', () => {
        apiWasCalled = true;
        return HttpResponse.json({ id: '1', username: 'test' });
      })
    );

    render(<UsersPage />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /create user/i })).toBeInTheDocument();
    });

    await user.click(screen.getByRole('button', { name: /create user/i }));

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });

    // Find the submit button within the dialog
    const dialog = screen.getByRole('dialog');
    const submitButton = dialog.querySelector('button[type="submit"]') ||
                         screen.getByRole('button', { name: /^create$/i });

    expect(submitButton).toBeInTheDocument();

    // Try to submit empty form
    await user.click(submitButton!);

    // Modal should remain open because form validation failed
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });

    // API should NOT have been called due to validation
    expect(apiWasCalled).toBe(false);
  });

  it('prevents form submission with short username', async () => {
    const user = userEvent.setup();

    let apiWasCalled = false;
    server.use(
      http.post('/api/v1/admin/users', () => {
        apiWasCalled = true;
        return HttpResponse.json({ id: '1', username: 'test' });
      })
    );

    render(<UsersPage />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /create user/i })).toBeInTheDocument();
    });

    await user.click(screen.getByRole('button', { name: /create user/i }));

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });

    // Enter short username (less than 3 characters)
    const usernameInput = screen.getByLabelText(/username/i);
    await user.type(usernameInput, 'ab');

    // Submit the form
    const submitButton = screen.getByRole('button', { name: /^create$/i });
    await user.click(submitButton);

    // Modal should remain open
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });

    // API should NOT have been called
    expect(apiWasCalled).toBe(false);
  });

  it('prevents form submission with short password', async () => {
    const user = userEvent.setup();

    let apiWasCalled = false;
    server.use(
      http.post('/api/v1/admin/users', () => {
        apiWasCalled = true;
        return HttpResponse.json({ id: '1', username: 'test' });
      })
    );

    render(<UsersPage />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /create user/i })).toBeInTheDocument();
    });

    await user.click(screen.getByRole('button', { name: /create user/i }));

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });

    // Enter valid username but short password
    const usernameInput = screen.getByLabelText(/username/i);
    const passwordInput = screen.getByLabelText(/password/i);
    await user.type(usernameInput, 'validuser');
    await user.type(passwordInput, 'short');

    // Submit the form
    const submitButton = screen.getByRole('button', { name: /^create$/i });
    await user.click(submitButton);

    // Modal should remain open
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });

    // API should NOT have been called
    expect(apiWasCalled).toBe(false);
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
