import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, within } from '../test/utils';
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

    it('navigates to settings from menu', async () => {
      const user = userEvent.setup();
      render(<DashboardLayout />);

      // Click on the avatar to open the menu
      await user.click(screen.getByText('A'));

      // Click on Settings in the dropdown
      const menuItems = await screen.findAllByText('Settings');
      // The second one is in the dropdown
      await user.click(menuItems[1]);

      expect(mockNavigate).toHaveBeenCalledWith('/settings');
    });
  });

  describe('logout', () => {
    beforeEach(() => {
      useAuthStore.setState({
        accessToken: 'test-token',
        refreshToken: 'test-refresh-token',
        user: { id: '1', username: 'admin', role: 'admin' },
        isAuthenticated: true,
      });
    });

    it('logs out when clicking logout', async () => {
      const user = userEvent.setup();
      render(<DashboardLayout />);

      // Click on the avatar to open the menu
      await user.click(screen.getByText('A'));

      // Click on Logout
      await user.click(screen.getByText('Logout'));

      expect(mockNavigate).toHaveBeenCalledWith('/login');
      expect(useAuthStore.getState().isAuthenticated).toBe(false);
      expect(useAuthStore.getState().user).toBeNull();
    });
  });
});
