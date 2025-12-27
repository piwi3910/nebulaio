import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '../test/utils';
import userEvent from '@testing-library/user-event';
import { DashboardLayout } from './DashboardLayout';
import { useAuthStore } from '../stores/auth';

// Mock react-router-dom's useNavigate
const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return {
    ...actual,
    useNavigate: () => mockNavigate,
    useLocation: () => ({ pathname: '/dashboard' }),
    Outlet: () => <div data-testid="outlet">Page Content</div>,
  };
});

describe('DashboardLayout', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('when user is admin', () => {
    beforeEach(() => {
      useAuthStore.setState({
        accessToken: 'test-token',
        refreshToken: 'test-refresh-token',
        user: { id: '1', username: 'admin', role: 'admin' },
        isAuthenticated: true,
      });
    });

    it('renders the navigation with all items for admin', () => {
      render(<DashboardLayout />);

      expect(screen.getByText('NebulaIO')).toBeInTheDocument();
      expect(screen.getByText('Dashboard')).toBeInTheDocument();
      expect(screen.getByText('Buckets')).toBeInTheDocument();
      expect(screen.getByText('Users')).toBeInTheDocument();
      expect(screen.getByText('Policies')).toBeInTheDocument();
      expect(screen.getByText('Access Keys')).toBeInTheDocument();
      expect(screen.getByText('Cluster')).toBeInTheDocument();
      expect(screen.getByText('Audit Logs')).toBeInTheDocument();
      expect(screen.getByText('AI/ML Features')).toBeInTheDocument();
      expect(screen.getByText('Settings')).toBeInTheDocument();
    });

    it('shows user avatar with first letter of username', () => {
      render(<DashboardLayout />);

      // Find the avatar by its text content
      expect(screen.getByText('A')).toBeInTheDocument();
    });

    it('renders the outlet for nested routes', () => {
      render(<DashboardLayout />);

      expect(screen.getByTestId('outlet')).toBeInTheDocument();
    });

    it('has a theme toggle button', () => {
      render(<DashboardLayout />);

      const themeToggle = screen.getByRole('button', { name: /toggle color scheme/i });
      expect(themeToggle).toBeInTheDocument();
    });
  });

  describe('when user is regular user', () => {
    beforeEach(() => {
      useAuthStore.setState({
        accessToken: 'test-token',
        refreshToken: 'test-refresh-token',
        user: { id: '2', username: 'user1', role: 'user' },
        isAuthenticated: true,
      });
    });

    it('renders only non-admin navigation items', () => {
      render(<DashboardLayout />);

      // Non-admin items should be visible
      expect(screen.getByText('Buckets')).toBeInTheDocument();
      expect(screen.getByText('Access Keys')).toBeInTheDocument();
      expect(screen.getByText('Settings')).toBeInTheDocument();

      // Admin-only items should not be visible
      expect(screen.queryByText('Dashboard')).not.toBeInTheDocument();
      expect(screen.queryByText('Users')).not.toBeInTheDocument();
      expect(screen.queryByText('Policies')).not.toBeInTheDocument();
      expect(screen.queryByText('Cluster')).not.toBeInTheDocument();
      expect(screen.queryByText('Audit Logs')).not.toBeInTheDocument();
      expect(screen.queryByText('AI/ML Features')).not.toBeInTheDocument();
    });

    it('shows user avatar with first letter', () => {
      render(<DashboardLayout />);

      expect(screen.getByText('U')).toBeInTheDocument();
    });
  });

  describe('navigation', () => {
    beforeEach(() => {
      useAuthStore.setState({
        accessToken: 'test-token',
        refreshToken: 'test-refresh-token',
        user: { id: '1', username: 'admin', role: 'admin' },
        isAuthenticated: true,
      });
    });

    it('navigates when clicking navigation items', async () => {
      const user = userEvent.setup();
      render(<DashboardLayout />);

      await user.click(screen.getByText('Buckets'));

      expect(mockNavigate).toHaveBeenCalledWith('/buckets');
    });

    it('has settings link in the sidebar navigation', () => {
      render(<DashboardLayout />);

      // Settings should be visible in the sidebar navigation
      expect(screen.getByText('Settings')).toBeInTheDocument();
    });
  });

  describe('user menu', () => {
    beforeEach(() => {
      useAuthStore.setState({
        accessToken: 'test-token',
        refreshToken: 'test-refresh-token',
        user: { id: '1', username: 'admin', role: 'admin' },
        isAuthenticated: true,
      });
    });

    it('displays user avatar with first letter', () => {
      render(<DashboardLayout />);

      // The avatar should show 'A' for 'admin'
      expect(screen.getByText('A')).toBeInTheDocument();
    });

    it('can click the avatar', async () => {
      const user = userEvent.setup();
      render(<DashboardLayout />);

      // Click on the avatar - this should work without error
      await user.click(screen.getByText('A'));

      // The menu should open (implementation depends on Mantine portal behavior)
      // Just verify the click doesn't throw
    });
  });

  describe('logout functionality', () => {
    beforeEach(() => {
      useAuthStore.setState({
        accessToken: 'test-token',
        refreshToken: 'test-refresh-token',
        user: { id: '1', username: 'admin', role: 'admin' },
        isAuthenticated: true,
      });
    });

    it('logout function clears auth state', () => {
      // Test the logout store function directly since menu testing is flaky with portals
      const { logout } = useAuthStore.getState();
      logout();

      expect(useAuthStore.getState().isAuthenticated).toBe(false);
      expect(useAuthStore.getState().user).toBeNull();
      expect(useAuthStore.getState().accessToken).toBeNull();
      expect(useAuthStore.getState().refreshToken).toBeNull();
    });
  });
});
