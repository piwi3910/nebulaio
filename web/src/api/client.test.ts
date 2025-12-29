import { describe, it, expect, beforeEach } from 'vitest';
import { adminApi, consoleApi, apiClient } from './client';
import { useAuthStore } from '../stores/auth';
import { server } from '../test/mocks/server';
import { http, HttpResponse } from 'msw';

describe('apiClient', () => {
  beforeEach(() => {
    useAuthStore.setState({
      accessToken: 'test-token',
      refreshToken: 'test-refresh-token',
      user: { id: '1', username: 'admin', role: 'admin' },
      isAuthenticated: true,
    });
  });

  describe('request interceptor', () => {
    it('adds authorization header when token exists', async () => {
      let capturedHeaders: Record<string, string> = {};

      server.use(
        http.get('/api/v1/test', ({ request }) => {
          capturedHeaders = Object.fromEntries(request.headers.entries());
          return HttpResponse.json({ success: true });
        })
      );

      await apiClient.get('/test');

      expect(capturedHeaders['authorization']).toBe('Bearer test-token');
    });

    it('does not add authorization header when no token', async () => {
      useAuthStore.setState({
        accessToken: null,
        refreshToken: null,
        user: null,
        isAuthenticated: false,
      });

      let capturedHeaders: Record<string, string> = {};

      server.use(
        http.get('/api/v1/test', ({ request }) => {
          capturedHeaders = Object.fromEntries(request.headers.entries());
          return HttpResponse.json({ success: true });
        })
      );

      await apiClient.get('/test');

      expect(capturedHeaders['authorization']).toBeUndefined();
    });
  });
});

describe('adminApi', () => {
  beforeEach(() => {
    useAuthStore.setState({
      accessToken: 'test-token',
      refreshToken: 'test-refresh-token',
      user: { id: '1', username: 'admin', role: 'admin' },
      isAuthenticated: true,
    });
  });

  describe('auth', () => {
    it('login calls correct endpoint', async () => {
      // Use test credentials matching mock handlers
      const response = await adminApi.login('admin', 'TestPassword123');

      expect(response.data).toHaveProperty('token');
      expect(response.data).toHaveProperty('user');
    });
  });

  describe('users', () => {
    it('listUsers returns users', async () => {
      const response = await adminApi.listUsers();

      // API returns array directly
      expect(response.data).toBeInstanceOf(Array);
      expect(response.data.length).toBeGreaterThan(0);
    });

    it('createUser sends correct data', async () => {
      const userData = {
        username: 'newuser',
        password: 'password123',
        email: 'new@example.com',
        role: 'user',
      };

      const response = await adminApi.createUser(userData);

      expect(response.status).toBe(201);
      expect(response.data.username).toBe(userData.username);
    });
  });

  describe('buckets', () => {
    it('listBuckets returns buckets', async () => {
      const response = await adminApi.listBuckets();

      // API returns array directly
      expect(response.data).toBeInstanceOf(Array);
      expect(response.data.length).toBeGreaterThan(0);
    });

    it('createBucket sends correct data', async () => {
      const response = await adminApi.createBucket({ name: 'new-bucket' });

      expect(response.status).toBe(201);
      expect(response.data.name).toBe('new-bucket');
    });
  });

  describe('cluster', () => {
    it('getClusterStatus returns status', async () => {
      const response = await adminApi.getClusterStatus();

      expect(response.data).toHaveProperty('status');
      expect(response.data).toHaveProperty('leader');
    });

    it('listNodes returns nodes', async () => {
      const response = await adminApi.listNodes();

      expect(response.data).toHaveProperty('nodes');
      expect(response.data.nodes).toBeInstanceOf(Array);
    });
  });

  describe('aiml', () => {
    it('getAIMLStatus returns feature status', async () => {
      const response = await adminApi.getAIMLStatus();

      expect(response.data).toHaveProperty('features');
      expect(response.data.features).toBeInstanceOf(Array);
    });

    it('getAIMLMetrics returns metrics', async () => {
      const response = await adminApi.getAIMLMetrics();

      expect(response.data).toHaveProperty('s3_express');
      expect(response.data).toHaveProperty('iceberg');
      expect(response.data).toHaveProperty('mcp');
    });
  });

  describe('config', () => {
    it('getConfig returns configuration', async () => {
      const response = await adminApi.getConfig();

      expect(response.data).toHaveProperty('s3_express');
      expect(response.data).toHaveProperty('iceberg');
    });

    it('updateConfig sends configuration', async () => {
      const response = await adminApi.updateConfig({
        s3_express: { enabled: true },
      });

      expect(response.data.status).toBe('ok');
    });
  });

  describe('audit logs', () => {
    it('listAuditLogs returns logs', async () => {
      const response = await adminApi.listAuditLogs();

      expect(response.data).toHaveProperty('logs');
      expect(response.data.logs).toBeInstanceOf(Array);
    });

    it('listAuditLogs accepts filter parameters', async () => {
      const response = await adminApi.listAuditLogs({
        bucket: 'test-bucket',
        user_id: '1',
        page: 1,
        page_size: 10,
      });

      expect(response.data).toHaveProperty('logs');
    });
  });
});

describe('consoleApi', () => {
  beforeEach(() => {
    useAuthStore.setState({
      accessToken: 'test-token',
      refreshToken: 'test-refresh-token',
      user: { id: '1', username: 'admin', role: 'admin' },
      isAuthenticated: true,
    });
  });

  describe('profile', () => {
    it('getCurrentUser returns user info', async () => {
      const response = await consoleApi.getCurrentUser();

      expect(response.data).toHaveProperty('id');
      expect(response.data).toHaveProperty('username');
    });
  });

  describe('bucket objects', () => {
    it('listBucketObjects returns objects', async () => {
      const response = await consoleApi.listBucketObjects('test-bucket');

      expect(response.data).toHaveProperty('objects');
      expect(response.data.objects).toBeInstanceOf(Array);
    });
  });
});
