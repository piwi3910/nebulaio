import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, cleanup } from '../test/utils';
import userEvent from '@testing-library/user-event';
import { LoginPage } from './LoginPage';
import { useAuthStore } from '../stores/auth';

// Mock react-router-dom's useNavigate
const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  };
});

describe('LoginPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // Reset auth store
    useAuthStore.setState({
      accessToken: null,
      refreshToken: null,
      user: null,
      isAuthenticated: false,
    });
  });

  afterEach(() => {
    cleanup();
  });

  it('renders the login form', () => {
    render(<LoginPage />);

    expect(screen.getByText('NebulaIO')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Sign in' })).toBeInTheDocument();
    expect(screen.getByLabelText(/username/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /sign in/i })).toBeInTheDocument();
  });

  it('validates that form inputs are required', () => {
    render(<LoginPage />);

    const usernameInput = screen.getByLabelText(/username/i);
    const passwordInput = screen.getByLabelText(/password/i);

    // Verify inputs have the required attribute
    expect(usernameInput).toHaveAttribute('required');
    expect(passwordInput).toHaveAttribute('required');

    // Verify initial values are empty
    expect(usernameInput).toHaveValue('');
    expect(passwordInput).toHaveValue('');
  });

  it('allows entering username and password', async () => {
    const user = userEvent.setup();
    render(<LoginPage />);

    const usernameInput = screen.getByLabelText(/username/i);
    const passwordInput = screen.getByLabelText(/password/i);

    await user.type(usernameInput, 'testuser');
    await user.type(passwordInput, 'testpass');

    expect(usernameInput).toHaveValue('testuser');
    expect(passwordInput).toHaveValue('testpass');
  });

  it('displays the NebulaIO branding', () => {
    render(<LoginPage />);

    expect(screen.getByText('NebulaIO')).toBeInTheDocument();
    expect(screen.getByText('Enter your credentials to access the console')).toBeInTheDocument();
  });

  it('has required attributes on form inputs', () => {
    render(<LoginPage />);

    const usernameInput = screen.getByLabelText(/username/i);
    const passwordInput = screen.getByLabelText(/password/i);

    expect(usernameInput).toHaveAttribute('required');
    expect(passwordInput).toHaveAttribute('required');
  });
});
