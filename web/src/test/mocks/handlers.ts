import { http, HttpResponse } from 'msw';

// Define handlers that mock the API responses
export const handlers = [
  // Auth endpoints
  http.post('/api/v1/admin/auth/login', async ({ request }) => {
    const body = await request.json() as { username: string; password: string };

    if (body.username === 'admin' && body.password === 'Admin123') {
      return HttpResponse.json({
        token: 'mock-jwt-token',
        refresh_token: 'mock-refresh-token',
        user: {
          id: '1',
          username: 'admin',
          role: 'admin',
        },
      });
    }

    return HttpResponse.json(
      { error: 'Invalid credentials' },
      { status: 401 }
    );
  }),

  http.post('/api/v1/admin/auth/refresh', () => {
    return HttpResponse.json({
      token: 'mock-new-jwt-token',
      refresh_token: 'mock-new-refresh-token',
    });
  }),

  // Users endpoints - return array directly
  http.get('/api/v1/admin/users', () => {
    return HttpResponse.json([
      { id: '1', username: 'admin', role: 'admin', created_at: '2024-01-01T00:00:00Z' },
      { id: '2', username: 'user1', role: 'user', created_at: '2024-01-02T00:00:00Z' },
    ]);
  }),

  http.post('/api/v1/admin/users', async ({ request }) => {
    const body = await request.json() as { username: string; password: string; role: string };
    return HttpResponse.json({
      id: '3',
      username: body.username,
      role: body.role,
      created_at: new Date().toISOString(),
    }, { status: 201 });
  }),

  // Buckets endpoints - return array directly
  http.get('/api/v1/admin/buckets', () => {
    return HttpResponse.json([
      { name: 'test-bucket', created_at: '2024-01-01T00:00:00Z', size: 1024000 },
      { name: 'data-bucket', created_at: '2024-01-02T00:00:00Z', size: 5120000 },
    ]);
  }),

  http.post('/api/v1/admin/buckets', async ({ request }) => {
    const body = await request.json() as { name: string };
    return HttpResponse.json({
      name: body.name,
      created_at: new Date().toISOString(),
      size: 0,
    }, { status: 201 });
  }),

  // Cluster endpoints
  http.get('/api/v1/admin/cluster/status', () => {
    return HttpResponse.json({
      status: 'healthy',
      leader: 'node-1',
      nodes: 3,
    });
  }),

  http.get('/api/v1/admin/cluster/nodes', () => {
    return HttpResponse.json({
      nodes: [
        { id: 'node-1', address: '10.0.0.1:9000', status: 'leader', healthy: true },
        { id: 'node-2', address: '10.0.0.2:9000', status: 'follower', healthy: true },
        { id: 'node-3', address: '10.0.0.3:9000', status: 'follower', healthy: true },
      ],
    });
  }),

  // AI/ML endpoints
  http.get('/api/v1/admin/aiml/status', () => {
    return HttpResponse.json({
      features: [
        { name: 's3_express', enabled: false, available: true, version: '1.0' },
        { name: 'iceberg', enabled: false, available: true, version: '1.0' },
        { name: 'mcp', enabled: false, available: true, version: '1.0' },
        { name: 'gpudirect', enabled: false, available: true, version: '1.0' },
        { name: 'dpu', enabled: false, available: true, version: '1.0' },
        { name: 'rdma', enabled: false, available: true, version: '1.0' },
        { name: 'nim', enabled: false, available: true, version: '1.0' },
      ],
    });
  }),

  http.get('/api/v1/admin/aiml/metrics', () => {
    return HttpResponse.json({
      s3_express: { sessions_active: 0, atomic_appends: 0 },
      iceberg: { tables_count: 0, transactions_total: 0 },
      mcp: { connections_active: 0, requests_total: 0 },
      gpudirect: { transfers_total: 0, bytes_transferred: 0 },
      dpu: { offload_operations: 0, crypto_operations: 0 },
      rdma: { connections_active: 0, operations_total: 0 },
      nim: { inference_requests: 0, tokens_used: 0 },
    });
  }),

  http.get('/api/v1/admin/config', () => {
    return HttpResponse.json({
      s3_express: { enabled: false, default_zone: 'use1-az1' },
      iceberg: { enabled: false, warehouse: 's3://warehouse/' },
      mcp: { enabled: false, port: 9005 },
      gpudirect: { enabled: false, buffer_pool_size: 1073741824 },
      dpu: { enabled: false, device_name: 'mlx5_0' },
      rdma: { enabled: false, port: 9100 },
      nim: { enabled: false, endpoint: 'https://integrate.api.nvidia.com' },
    });
  }),

  http.put('/api/v1/admin/config', () => {
    return HttpResponse.json({
      status: 'ok',
      message: 'Configuration updated. Restart required for changes to take effect.',
    });
  }),

  // Audit logs - keep nested format with correct field names
  http.get('/api/v1/admin/audit-logs', () => {
    return HttpResponse.json({
      logs: [
        {
          id: '1',
          timestamp: '2024-01-01T12:00:00Z',
          event_type: 'user:login',
          user_id: 'user-1',
          username: 'admin',
          source_ip: '192.168.1.100',
          user_agent: 'Mozilla/5.0',
          request_id: 'req-123',
          status_code: 200,
          details: {},
        },
        {
          id: '2',
          timestamp: '2024-01-01T12:05:00Z',
          event_type: 'bucket:create',
          user_id: 'user-1',
          username: 'admin',
          source_ip: '192.168.1.100',
          user_agent: 'Mozilla/5.0',
          request_id: 'req-124',
          status_code: 201,
          details: { bucket: 'test-bucket' },
        },
      ],
      total: 2,
      page: 1,
      page_size: 25,
      total_pages: 1,
    });
  }),

  // Policies
  http.get('/api/v1/admin/policies', () => {
    return HttpResponse.json({
      policies: [
        { name: 'readonly', policy: '{"Version":"2012-10-17","Statement":[]}' },
        { name: 'readwrite', policy: '{"Version":"2012-10-17","Statement":[]}' },
      ],
    });
  }),

  // Console endpoints
  http.get('/api/v1/console/me', () => {
    return HttpResponse.json({
      id: '1',
      username: 'admin',
      role: 'admin',
    });
  }),

  // Console access keys - return array directly
  http.get('/api/v1/console/me/keys', () => {
    return HttpResponse.json([]);
  }),

  // Console buckets - return array directly
  http.get('/api/v1/console/buckets', () => {
    return HttpResponse.json([
      { name: 'my-bucket', created_at: '2024-01-01T00:00:00Z', size: 1024000 },
    ]);
  }),

  http.get('/api/v1/console/buckets/:bucket/objects', () => {
    return HttpResponse.json({
      objects: [
        { key: 'file1.txt', size: 1024, last_modified: '2024-01-01T00:00:00Z' },
        { key: 'file2.json', size: 2048, last_modified: '2024-01-02T00:00:00Z' },
      ],
      prefix: '',
      delimiter: '/',
    });
  }),

  // Health endpoint
  http.get('/health', () => {
    return HttpResponse.json({ status: 'ok' });
  }),
];
