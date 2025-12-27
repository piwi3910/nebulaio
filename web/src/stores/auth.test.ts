import { describe, it, expect, beforeEach } from 'vitest';
import { useAuthStore } from './auth';

describe('useAuthStore', () => {
  beforeEach(() => {
    // Reset store to initial state
    useAuthStore.setState({
      accessToken: null,
      refreshToken: null,
      user: null,
      isAuthenticated: false,
    });
  });

  describe('initial state', () => {
    it('starts with no authentication', () => {
      const state = useAuthStore.getState();

      expect(state.accessToken).toBeNull();
      expect(state.refreshToken).toBeNull();
      expect(state.user).toBeNull();
      expect(state.isAuthenticated).toBe(false);
    });
  });

  describe('setTokens', () => {
    it('sets access and refresh tokens', () => {
      useAuthStore.getState().setTokens('access-token', 'refresh-token');

      const state = useAuthStore.getState();
      expect(state.accessToken).toBe('access-token');
      expect(state.refreshToken).toBe('refresh-token');
      expect(state.isAuthenticated).toBe(true);
    });
  });

  describe('setUser', () => {
    it('sets the user object', () => {
      const user = {
        id: '1',
        username: 'testuser',
        role: 'admin' as const,
      };

      useAuthStore.getState().setUser(user);

      const state = useAuthStore.getState();
      expect(state.user).toEqual(user);
    });

    it('sets user with optional email', () => {
      const user = {
        id: '1',
        username: 'testuser',
        email: 'test@example.com',
        role: 'user' as const,
      };

      useAuthStore.getState().setUser(user);

      const state = useAuthStore.getState();
      expect(state.user?.email).toBe('test@example.com');
    });
  });

  describe('logout', () => {
    it('clears all auth state', () => {
      // First set up authenticated state
      useAuthStore.getState().setTokens('access-token', 'refresh-token');
      useAuthStore.getState().setUser({
        id: '1',
        username: 'testuser',
        role: 'admin',
      });

      // Then logout
      useAuthStore.getState().logout();

      const state = useAuthStore.getState();
      expect(state.accessToken).toBeNull();
      expect(state.refreshToken).toBeNull();
      expect(state.user).toBeNull();
      expect(state.isAuthenticated).toBe(false);
    });
  });

  describe('authentication flow', () => {
    it('handles full login flow', () => {
      const store = useAuthStore.getState();

      // Login
      store.setTokens('access-token', 'refresh-token');
      store.setUser({
        id: '1',
        username: 'admin',
        role: 'superadmin',
      });

      let state = useAuthStore.getState();
      expect(state.isAuthenticated).toBe(true);
      expect(state.user?.username).toBe('admin');
      expect(state.user?.role).toBe('superadmin');

      // Logout
      store.logout();

      state = useAuthStore.getState();
      expect(state.isAuthenticated).toBe(false);
      expect(state.user).toBeNull();
    });
  });

  describe('user roles', () => {
    it.each([
      'superadmin',
      'admin',
      'user',
      'readonly',
      'service',
    ] as const)('accepts %s role', (role) => {
      useAuthStore.getState().setUser({
        id: '1',
        username: 'testuser',
        role,
      });

      const state = useAuthStore.getState();
      expect(state.user?.role).toBe(role);
    });
  });
});
